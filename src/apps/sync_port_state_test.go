package apps

import (
	"errors"
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

	var savedPorts []interface{}
	mockTunnel.EXPECT().SaveRemotePorts(mock.Anything).RunAndReturn(func(p common.TransitionPayload) error {
		savedPorts = p.Ports
		return nil
	}).Once()
	mockTunnel.EXPECT().GetState().Return([]tunnel.TunnelState{}, nil).Once()

	require.NoError(t, am.syncPortState(payload, app))

	savedRules := spsRules(t, savedPorts)
	require.Len(t, savedRules, 1)
	assert.Zero(t, savedRules[0].HostPort, "no host port must be invented before launch")
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

	contents, skipped, err := am.StateMachine.generateDotEnvContents(cfg, payload, app)
	require.NoError(t, err)
	assert.Empty(t, skipped)
	assert.Contains(t, contents, "WG_PORT_CLOUD=31099")
	// No local tunnel object -> the base WG_PORT env is absent; the cloud
	// port must not depend on it.
	assert.NotContains(t, contents, "\nWG_PORT=")
}
