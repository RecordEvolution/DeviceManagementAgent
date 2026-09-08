package apps

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"reagent/common"
	"reagent/errdefs"
	"reagent/tunnel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// spsPorts builds the payload.Ports value for a single active rule.
func spsPorts(t *testing.T, rule common.PortForwardRule) []interface{} {
	t.Helper()
	ports, err := tunnel.PortForwardRuleToInterface([]common.PortForwardRule{rule})
	require.NoError(t, err)
	return ports
}

func spsRules(t *testing.T, ports []interface{}) []common.PortForwardRule {
	t.Helper()
	rules, err := tunnel.InterfaceToPortForwardRule(ports)
	require.NoError(t, err)
	return rules
}

// TestSyncPortStateUsesManagedHostPort: the tunnel dials the agent-managed
// host port while the subdomain keeps the declared port, and the assignment
// is persisted as host_port on the saved rules.
func TestSyncPortStateUsesManagedHostPort(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 7, "portapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(7, "portapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "web", Port: 8080, Protocol: "http", Active: true})

	// The launch path allocated a managed host port for the declared port.
	hostPort, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 7, Protocol: "tcp", Port: 8080}, 41234)
	require.NoError(t, err)
	require.Equal(t, uint64(41234), hostPort)

	expectedSubdomain := tunnel.CreateSubdomain(tunnel.Protocol("http"), uint64(cfg.ReswarmConfig.DeviceKey), "portapp", 8080)
	tunnelID := tunnel.CreateTunnelID(expectedSubdomain, "http")

	mockTunnel.EXPECT().Get(tunnelID).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, expectedSubdomain, conf.Subdomain, "subdomain must keep the declared port")
		assert.Equal(t, uint64(41234), conf.LocalPort, "frpc must dial the managed host port")
		assert.Equal(t, "", conf.LocalIP)
		conf.RemotePort = 30123
		return conf, nil
	}).Once()

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(8080), savedRules[0].Port, "declared port stays the identity")
	assert.Equal(t, uint64(41234), savedRules[0].HostPort, "host port is persisted upstream")
	assert.Equal(t, uint64(30123), savedRules[0].RemotePort)
}

// TestSyncPortStateDefersFreshApp: nothing allocated, no container, nothing
// persisted — tunnel creation waits for the post-transition sync.
func TestSyncPortStateDefersFreshApp(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, _ := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 8, "freshapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(8, "freshapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "web", Port: 8080, Protocol: "http", Active: true})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(nil, errdefs.ContainerNotFound(errors.New("not found"))).Once()

	// SaveRemotePorts is deliberately not expected: nothing changed against the
	// rules the cloud just sent, so there is nothing to persist upstream (a
	// strict mock fails the test on any call).
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))
}

// TestSyncPortStateLegacyHostNetworking: a pre-migration container running
// with host networking keeps its declared-port tunnel.
func TestSyncPortStateLegacyHostNetworking(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, _ := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 9, "legacyapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(9, "legacyapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "web", Port: 8080, Protocol: "http", Active: true})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{}, nil).Once()
	mockContainer.EXPECT().GetContainerNetworkMode(mock.Anything, payload.ContainerName.Prod).
		Return("host", nil).Once()

	mockTunnel.EXPECT().Get(mock.Anything).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(8080), conf.LocalPort, "legacy host networking dials the declared port")
		return conf, nil
	}).Once()
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).Return(nil).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	// The legacy port is not a managed assignment.
	_, assigned := am.hostPorts.GetByPort(common.PROD, 9, "tcp", 8080)
	assert.False(t, assigned)
}

// TestSyncPortStateReplacesStaleTunnel: an in-memory tunnel dialing an
// outdated host port is removed and re-added with the current one.
func TestSyncPortStateReplacesStaleTunnel(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 10, "staleapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(10, "staleapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "web", Port: 8080, Protocol: "http", Active: true})

	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 10, Protocol: "tcp", Port: 8080}, 41300)
	require.NoError(t, err)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("http"), uint64(cfg.ReswarmConfig.DeviceKey), "staleapp", 8080)
	staleConfig := tunnel.TunnelConfig{Subdomain: subdomain, Protocol: tunnel.Protocol("http"), LocalPort: 40000}

	mockTunnel.EXPECT().Get(mock.Anything).Return(&tunnel.Tunnel{Config: staleConfig}).Once()
	mockTunnel.EXPECT().RemoveTunnel(staleConfig).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(41300), conf.LocalPort)
		return conf, nil
	}).Once()
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).Return(nil).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))
}

// TestSyncPortStateKeepsReservedRemotePort: when the tunnel already dials the
// right host port the add is skipped, but the remote port frps reserved must
// still reach the persisted rule. The cloud does not know it (it sends 0), so
// dropping it here publishes remote_port 0 upstream and makes the next agent
// start reserve a different port.
func TestSyncPortStateKeepsReservedRemotePort(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 12, "mqttapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(12, "mqttapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true})

	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 12, Protocol: "tcp", Port: 1883}, 40001)
	require.NoError(t, err)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "mqttapp", 1883)
	existing := tunnel.TunnelConfig{
		Subdomain:  subdomain,
		Protocol:   tunnel.Protocol("tcp"),
		LocalPort:  40001,
		RemotePort: 30001, // granted by frps when the tunnel was first added
	}

	// Already dialing the current host port -> AddTunnel must not be called.
	mockTunnel.EXPECT().Get(tunnel.CreateTunnelID(subdomain, "tcp")).Return(&tunnel.Tunnel{Config: existing}).Once()
	// Skip-add now requires a LIVE frpc proxy, not just bookkeeping.
	mockTunnel.EXPECT().Status(tunnel.CreateTunnelID(subdomain, "tcp")).Return(tunnel.TunnelStatus{Status: "running"}, nil).Once()

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(30001), savedRules[0].RemotePort, "the reserved remote port must survive a re-sync")
	assert.Equal(t, uint64(40001), savedRules[0].HostPort)
}

// TestSyncPortStatePersistsHostPortWithoutTunnels: a device that cannot tunnel
// (frpc missing/quarantined, or frps unreachable) still publishes its apps on
// host ports, and the LAN address is then the only way in — so the assignment
// must reach the cloud. Only the frpc calls are skipped.
func TestSyncPortStatePersistsHostPortWithoutTunnels(t *testing.T) {
	am, _, mockTunnel, appStore, _, _ := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(false).Maybe()

	app := amSeed(t, appStore, 13, "notunnelapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(13, "notunnelapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "web", Port: 8080, Protocol: "http", Active: true})

	// The launch path allocated a managed host port; it does not depend on
	// tunnels being available.
	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 13, Protocol: "tcp", Port: 8080}, 41999)
	require.NoError(t, err)

	// Get/AddTunnel/RemoveTunnel/GetState are deliberately not expected: any
	// call would reach an frpc that cannot serve it.
	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(41999), savedRules[0].HostPort, "host port must be persisted without tunnels")
	assert.Zero(t, savedRules[0].RemotePort, "no tunnel means no remote port")
}

// TestSyncPortStateNoRulesIsANoop: an app that exposes nothing must not cost a
// round trip on every state request.
func TestSyncPortStateNoRulesIsANoop(t *testing.T) {
	am, _, mockTunnel, appStore, _, _ := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 14, "portlessapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(14, "portlessapp", common.RUNNING, common.PROD)
	payload.Ports = nil

	// No SaveRemotePorts/GetState expectations: nothing may be called.
	require.NoError(t, am.syncPortState(payload, app))
}

// TestGenerateDotEnvContentsCloudRemotePort: an instance-patched cloud port
// reaches the compose dotenv as {RemotePortEnvironment}_CLOUD even when no
// local tunnel object exists (the value is payload-borne).
func TestGenerateDotEnvContentsCloudRemotePort(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().Get(mock.Anything).Return(nil).Maybe()

	app := amSeed(t, appStore, 11, "vpnapp", common.PRESENT, common.PROD)
	payload := amPayload(11, "vpnapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName:              "vpn",
		Port:                  51820,
		Protocol:              "udp",
		Active:                true,
		RemotePortEnvironment: "WG_PORT",
		CloudRemotePort:       31099,
	})

	contents, skipped, err := am.StateMachine.generateDotEnvContents(cfg, payload, app, payload.DockerCompose)
	require.NoError(t, err)
	assert.Empty(t, skipped)
	assert.Contains(t, contents, "WG_PORT_CLOUD=31099")
	// The canonical name is emitted alongside the custom one.
	assert.Contains(t, contents, "REMOTE_PORT_FOR_51820_CLOUD=31099")
	// No local tunnel object -> the base WG_PORT env is absent; the cloud
	// port must not depend on it.
	assert.NotContains(t, contents, "\nWG_PORT=")
	assert.NotContains(t, contents, "REMOTE_PORT_FOR_51820=")
}

// The remove-and-reinstall deadlock: bookkeeping says the tunnel exists (same
// tunnel id, same host port), but frpc has no live proxy — it was dropped with
// the previous installation. The old code trusted the bookkeeping, skipped the
// add on every restart, and nothing ever re-established the tunnel. Skip-add
// must demand a LIVE proxy; a dead one gets replaced.
func TestSyncPortStateRebuildsDeadTunnel(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 21, "deadtunnel", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(21, "deadtunnel", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "ui", Port: 9090, Protocol: "http", Active: true})

	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 21, Protocol: "tcp", Port: 9090}, 41500)
	require.NoError(t, err)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("http"), uint64(cfg.ReswarmConfig.DeviceKey), "deadtunnel", 9090)
	tunnelID := tunnel.CreateTunnelID(subdomain, "http")
	stale := tunnel.TunnelConfig{Subdomain: subdomain, AppName: "deadtunnel", Protocol: tunnel.Protocol("http"), LocalPort: 41500}

	// Bookkeeping claims the tunnel is up on the SAME host port…
	mockTunnel.EXPECT().Get(tunnelID).Return(&tunnel.Tunnel{Config: stale}).Once()
	// …but frpc has no such proxy.
	mockTunnel.EXPECT().Status(tunnelID).Return(tunnel.TunnelStatus{}, errdefs.ErrNotFound).Once()

	// The dead entry must be replaced, not trusted.
	mockTunnel.EXPECT().RemoveTunnel(stale).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(41500), conf.LocalPort)
		return conf, nil
	}).Once()
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).Return(nil).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))
}

// Uninstall must tear down every tunnel the app owns — leaving them standing
// is what made the later reinstall skip recreating them.
func TestUninstallRemovesAppTunnels(t *testing.T) {
	am, _, mockTunnel, _, _, cfg := amHarness(t)
	_ = cfg

	mockTunnel.EXPECT().TunnelCapable().Return(true).Once()

	mine := tunnel.TunnelConfig{Subdomain: "sub-a", AppName: "victim", Protocol: tunnel.Protocol("http"), LocalPort: 41000}
	other := tunnel.TunnelConfig{Subdomain: "sub-b", AppName: "bystander", Protocol: tunnel.Protocol("tcp"), LocalPort: 41001}
	mockTunnel.EXPECT().GetTunnelConfig().Return([]tunnel.TunnelConfig{mine, other}, nil).Once()

	// Only the uninstalled app's tunnel is removed; the bystander stays.
	mockTunnel.EXPECT().RemoveTunnel(mine).Return(nil).Once()

	am.RemoveAppTunnels("victim")
}

// A tunnel frps keeps refusing must be retried, but PACED. The refusal is
// invisible to AddTunnel (it returns success once frpc reloads its config), so
// without a backoff every sync rebuilds the same dead proxy — rewriting
// frpc.yaml and re-driving NewProxy several times a second, hardest exactly
// when the appliance-side cause ("authorization check failed") is least able to
// clear. Observed on tls-sf012, 2026-08-28.
func TestSyncPortStatePacesRepeatedDeadTunnelRebuilds(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 22, "flappy", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(22, "flappy", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{RuleName: "ui", Port: 9091, Protocol: "http", Active: true})

	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 22, Protocol: "tcp", Port: 9091}, 41501)
	require.NoError(t, err)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("http"), uint64(cfg.ReswarmConfig.DeviceKey), "flappy", 9091)
	tunnelID := tunnel.CreateTunnelID(subdomain, "http")
	stale := tunnel.TunnelConfig{Subdomain: subdomain, AppName: "flappy", Protocol: tunnel.Protocol("http"), LocalPort: 41501, RemotePort: 30222}

	mockTunnel.EXPECT().Get(tunnelID).Return(&tunnel.Tunnel{Config: stale})
	// frps refused this proxy; frpc records the reason on the status. The
	// closure lets the test flip the proxy to healthy later on.
	proxyUp := false
	mockTunnel.EXPECT().Status(tunnelID).RunAndReturn(func(string) (tunnel.TunnelStatus, error) {
		if proxyUp {
			return tunnel.TunnelStatus{Name: tunnelID, Status: "running"}, nil
		}
		return tunnel.TunnelStatus{Name: tunnelID, Status: "start error", Error: "authorization check failed"}, nil
	})
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil)

	// Every pass persists; keep the last rules so the deferred passes can be
	// checked for the reserved remote port.
	var saved []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		saved = p.Ports
		return nil
	})

	// Exactly ONE rebuild across three back-to-back syncs.
	mockTunnel.EXPECT().RemoveTunnel(stale).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		return conf, nil
	}).Once()

	for i := 0; i < 3; i++ {
		require.NoError(t, am.syncPortState(payload, app))
	}
	mockTunnel.AssertExpectations(t)

	// Backing off must not clobber the reserved remote port with a zero — that
	// both hides the address upstream and makes the next start reserve another.
	rules := spsRules(t, saved)
	require.Len(t, rules, 1)
	assert.Equal(t, uint64(30222), rules[0].RemotePort, "a deferred rebuild must keep the reserved remote port")

	// Once the proxy comes up the backoff is cleared, so the NEXT failure
	// retries immediately rather than inheriting a stale delay.
	proxyUp = true
	require.NoError(t, am.syncPortState(payload, app))

	ok, _ := am.mayRebuildTunnel(tunnelID)
	assert.True(t, ok, "a healthy proxy must reset the rebuild backoff")
}

// =============================================================================
// User-reserved remote ports
// =============================================================================

// The reservation drives the tunnel config (strict mode) and a stale
// reservation_error is cleared on a matching grant, while reserved_* keys
// round-trip untouched.
func TestSyncPortStateReservedRemoteDrivesConfig(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 40, "reservedapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(40, "reservedapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
		ReservationError: "stale failure from a previous pass",
	})

	// The container already publishes the reserved port (recreated since the
	// reservation), so the reservation is active, not pending.
	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 15000}, nil).Once()

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "reservedapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")

	mockTunnel.EXPECT().Get(tunnelID).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(15000), conf.LocalPort, "frpc must dial the reserved host port")
		assert.Equal(t, uint64(30500), conf.RemotePort, "the reservation drives the requested remote port")
		assert.True(t, conf.Reserved, "a reserved rule must request strict validation")
		return conf, nil
	}).Once()

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(30500), savedRules[0].RemotePort)
	assert.Equal(t, uint64(15000), savedRules[0].HostPort)
	assert.Equal(t, uint64(15000), savedRules[0].ReservedHostPort, "reserved_* round-trips untouched")
	assert.Equal(t, uint64(30500), savedRules[0].ReservedRemotePort)
	assert.Empty(t, savedRules[0].ReservationError, "a matching grant clears the stale error")
}

// A LIVE tunnel sitting on the wrong remote port is stale once a reservation
// applies: applying/changing a reservation must move it (Remove+Add) so the
// old frps bind is dropped promptly.
func TestSyncPortStateMovesLiveTunnelToReservedRemote(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 41, "moveapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(41, "moveapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true, RemotePort: 30001,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
	})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 15000}, nil).Once()

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "moveapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")
	existing := tunnel.TunnelConfig{Subdomain: subdomain, AppName: "moveapp", Protocol: tunnel.Protocol("tcp"), LocalPort: 15000, RemotePort: 30001}

	// Same local port, wrong remote port: no liveness probe, straight replace.
	mockTunnel.EXPECT().Get(tunnelID).Return(&tunnel.Tunnel{Config: existing}).Once()
	mockTunnel.EXPECT().RemoveTunnel(existing).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(30500), conf.RemotePort)
		assert.True(t, conf.Reserved)
		return conf, nil
	}).Once()

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(30500), savedRules[0].RemotePort)
}

// A grant contradicting the reservation must never be persisted: the reserved
// value stays the desired state and the mismatch surfaces as reservation_error.
func TestSyncPortStateWriteBackGuardKeepsReservedRemote(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 42, "guardapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(42, "guardapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
	})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 15000}, nil).Once()

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "guardapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")

	mockTunnel.EXPECT().Get(tunnelID).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		conf.RemotePort = 31000 // frps handed out something else
		return conf, nil
	}).Once()

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Equal(t, uint64(30500), savedRules[0].RemotePort, "the granted 31000 must not clobber the reservation")
	assert.Contains(t, savedRules[0].ReservationError, "granted port 31000 instead of reserved 30500")
}

// A failed AddTunnel on a reserved rule must surface on the rule instead of
// being log-only (the UI row would spin forever).
func TestSyncPortStateReservedAddFailureSetsReservationError(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, msg, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 43, "failapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(43, "failapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
	})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 15000}, nil).Times(2)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "failapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")

	mockTunnel.EXPECT().Get(tunnelID).Return(nil).Times(2)
	mockTunnel.EXPECT().AddTunnel(mock.Anything).Return(tunnel.TunnelConfig{}, errors.New("port 30500 already in use")).Times(2)

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Times(2)

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Contains(t, savedRules[0].ReservationError, "already in use")
	assert.Zero(t, savedRules[0].RemotePort, "no grant may be invented on failure")

	countFailureLines := func() int {
		count := 0
		for _, call := range msg.GetPublishCalls() {
			if len(call.Args) == 0 {
				continue
			}
			if dict, ok := call.Args[0].(common.Dict); ok {
				if chunk, ok := dict["chunk"].(string); ok && strings.Contains(chunk, "could not be established") {
					count++
				}
			}
		}
		return count
	}
	assert.Equal(t, 1, countFailureLines(), "the failure is written to the app log")

	// The next sync pass carries the persisted reservation_error back in; the
	// identical failure must not write the app-log line again (a persistent
	// frps refusal would otherwise spam the user-visible log once per sync).
	// SaveRemotePorts is not expected again: nothing changed.
	payload2 := amPayload(43, "failapp", common.RUNNING, common.PROD)
	payload2.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		HostPort:         15000,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
		ReservationError: savedRules[0].ReservationError,
	})
	require.NoError(t, am.syncPortState(payload2, app))
	assert.Equal(t, 1, countFailureLines(), "an identical persisting failure logs once")
}

// A fully satisfied reservation round-trips as a no-op: nothing changed, so
// nothing is persisted upstream.
func TestSyncPortStateReservedNoopRoundTrip(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 44, "noopapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(44, "noopapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		HostPort: 15000, RemotePort: 30500,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
	})

	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 15000}, nil).Once()

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "noopapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")
	existing := tunnel.TunnelConfig{Subdomain: subdomain, AppName: "noopapp", Protocol: tunnel.Protocol("tcp"), LocalPort: 15000, RemotePort: 30500}

	mockTunnel.EXPECT().Get(tunnelID).Return(&tunnel.Tunnel{Config: existing}).Once()
	mockTunnel.EXPECT().Status(tunnelID).Return(tunnel.TunnelStatus{Status: "running"}, nil).Once()

	// SaveRemotePorts deliberately not expected: the strict mock fails on any
	// unexpected call.
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))
}

// http/https rules and their reserved_* keys are inert: no reserved remote is
// requested, and the host port stays pool-managed.
func TestSyncPortStateHttpRuleIgnoresReservations(t *testing.T) {
	am, _, mockTunnel, appStore, _, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 45, "webapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	payload := amPayload(45, "webapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, common.PortForwardRule{
		RuleName: "web", Port: 8080, Protocol: "http", Active: true,
		ReservedHostPort: 15000, ReservedRemotePort: 30500,
	})

	// The launch path allocated a pool port; the "reservation" must not move it.
	_, err := am.hostPorts.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 45, Protocol: "tcp", Port: 8080}, 41234)
	require.NoError(t, err)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("http"), uint64(cfg.ReswarmConfig.DeviceKey), "webapp", 8080)
	tunnelID := tunnel.CreateTunnelID(subdomain, "http")

	mockTunnel.EXPECT().Get(tunnelID).Return(nil).Once()
	mockTunnel.EXPECT().AddTunnel(mock.Anything).RunAndReturn(func(conf tunnel.TunnelConfig) (tunnel.TunnelConfig, error) {
		assert.Equal(t, uint64(41234), conf.LocalPort, "the pool port stays; reserved_* is inert on http")
		assert.Zero(t, conf.RemotePort)
		assert.False(t, conf.Reserved)
		return conf, nil
	}).Once()

	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).Return(nil).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))
}

// A host-port reservation saved while the app is RUNNING arrives via a sync
// that never recreates the container, so nothing binds the reserved port yet.
// The working tunnel must keep dialing the port the container actually
// publishes (host_port write-back stays truthful, the UI shows the
// reservation as pending), while the reserved port is pinned so the pool
// cannot hand it to another app meanwhile. The friendly "takes effect on next
// restart" app-log note is written once, not on every sync pass.
func TestSyncPortStateReservationOnRunningAppKeepsLiveDialPort(t *testing.T) {
	am, mockContainer, mockTunnel, appStore, msg, cfg := amHarness(t)

	mockTunnel.EXPECT().TunnelCapable().Return(true).Maybe()

	app := amSeed(t, appStore, 46, "pendingapp", common.RUNNING, common.PROD)
	app.RequestedState = common.RUNNING

	rule := common.PortForwardRule{
		RuleName: "mqtt", Port: 1883, Protocol: "tcp", Active: true,
		HostPort: 41234, RemotePort: 30001,
		ReservedHostPort: 15000,
	}
	payload := amPayload(46, "pendingapp", common.RUNNING, common.PROD)
	payload.Ports = spsPorts(t, rule)

	// The running container still publishes its pre-reservation pool port.
	mockContainer.EXPECT().GetContainerPortBindings(mock.Anything, payload.ContainerName.Prod).
		Return(map[string]uint64{"1883/tcp": 41234}, nil).Times(3)

	subdomain := tunnel.CreateSubdomain(tunnel.Protocol("tcp"), uint64(cfg.ReswarmConfig.DeviceKey), "pendingapp", 1883)
	tunnelID := tunnel.CreateTunnelID(subdomain, "tcp")
	existing := tunnel.TunnelConfig{Subdomain: subdomain, AppName: "pendingapp", Protocol: tunnel.Protocol("tcp"), LocalPort: 41234, RemotePort: 30001}

	// The live tunnel keeps dialing the published port: no Remove/Add, and
	// nothing changed, so nothing is persisted (SaveRemotePorts not expected).
	mockTunnel.EXPECT().Get(tunnelID).Return(&tunnel.Tunnel{Config: existing}).Times(3)
	mockTunnel.EXPECT().Status(tunnelID).Return(tunnel.TunnelStatus{Status: "running"}, nil).Times(3)
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Times(3)

	require.NoError(t, am.syncPortState(payload, app))

	// The reserved port stays pinned for the app so the pool cannot hand it
	// out while the reservation waits for the next container recreate.
	key := hostPortKeyForRule(common.PROD, 46, rule)
	assert.True(t, am.hostPorts.IsPinned(key))
	pinnedPort, ok := am.hostPorts.Get(key)
	require.True(t, ok)
	assert.Equal(t, uint64(15000), pinnedPort)

	logTopic := fmt.Sprintf("reswarm.logs.%s.%s", cfg.ReswarmConfig.SerialNumber, payload.ContainerName.Prod)
	countNotes := func() int {
		count := 0
		for _, call := range msg.GetPublishCalls() {
			if string(call.Topic) != logTopic || len(call.Args) == 0 {
				continue
			}
			if dict, ok := call.Args[0].(common.Dict); ok {
				if chunk, ok := dict["chunk"].(string); ok && strings.Contains(chunk, "takes effect on the next restart") {
					count++
				}
			}
		}
		return count
	}
	assert.Equal(t, 1, countNotes(), "the pending note is written on the first pass")

	// An identical second sync pass must not repeat the note (no app-log spam
	// while the reservation stays pending) and must keep the dial port.
	payload2 := amPayload(46, "pendingapp", common.RUNNING, common.PROD)
	payload2.Ports = spsPorts(t, rule)
	require.NoError(t, am.syncPortState(payload2, app))
	assert.Equal(t, 1, countNotes(), "a persisting pending reservation logs once")

	// Removing the reservation while it was still pending must drop the pin
	// and keep dialing the live port: recovering the never-bound 15000 here
	// would break the working tunnel exactly like dialing it directly would.
	ruleCleared := rule
	ruleCleared.ReservedHostPort = 0
	payload3 := amPayload(46, "pendingapp", common.RUNNING, common.PROD)
	payload3.Ports = spsPorts(t, ruleCleared)
	require.NoError(t, am.syncPortState(payload3, app))

	assert.False(t, am.hostPorts.IsPinned(key), "the stale pin is dropped with the reservation")
	recovered, ok := am.hostPorts.Get(key)
	require.True(t, ok)
	assert.Equal(t, uint64(41234), recovered, "the assignment reverts to the live pool port")
}
