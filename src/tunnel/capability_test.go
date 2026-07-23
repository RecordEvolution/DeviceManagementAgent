package tunnel

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func newCapabilityTestManager(t *testing.T) *FrpTunnelManager {
	t.Helper()
	cfg := &config.Config{
		CommandLineArguments: &config.CommandLineArguments{AgentDir: t.TempDir()},
		ReswarmConfig:        &config.ReswarmConfig{Environment: string(common.PRODUCTION)},
	}
	m, err := NewFrpTunnelManager(messenger.NewOffline(cfg), cfg)
	require.NoError(t, err)
	return m
}

// A freshly constructed manager is "capable" (not yet definitively
// unavailable), so the Linux boot path — where an app can reconcile before
// frpc finishes logging in — is not skipped.
func TestCapabilityDefaultsToCapable(t *testing.T) {
	m := newCapabilityTestManager(t)
	assert.True(t, m.TunnelCapable(), "unknown capability must count as capable")

	cap, _ := m.Capability()
	assert.Equal(t, CapabilityUnknown, cap)
}

func TestTunnelCapableOnlyFalseWhenUnavailable(t *testing.T) {
	m := newCapabilityTestManager(t)

	m.setCapability(CapabilityStarting, nil)
	assert.True(t, m.TunnelCapable(), "starting is still capable")

	m.setCapability(CapabilityAvailable, nil)
	assert.True(t, m.TunnelCapable())

	m.setCapability(CapabilityUnavailable, errors.New("frpc gone"))
	assert.False(t, m.TunnelCapable(), "only unavailable is incapable")

	cap, lastErr := m.Capability()
	assert.Equal(t, CapabilityUnavailable, cap)
	assert.Equal(t, "frpc gone", lastErr)
}

func TestMarkUnavailable(t *testing.T) {
	m := newCapabilityTestManager(t)
	m.MarkUnavailable("tunnels are not yet supported on Windows")

	assert.False(t, m.TunnelCapable())
	_, lastErr := m.Capability()
	assert.Equal(t, "tunnels are not yet supported on Windows", lastErr)
}

func TestBecomingAvailableClearsLastErr(t *testing.T) {
	m := newCapabilityTestManager(t)
	m.setCapability(CapabilityUnavailable, errors.New("boom"))
	m.setCapability(CapabilityAvailable, nil)

	_, lastErr := m.Capability()
	assert.Empty(t, lastErr)
}

// ensureFrpcBinary re-acquires a missing binary via the injected callback, so
// a runtime quarantine (antivirus deleting frpc.exe) is recoverable instead of
// a permanent crash-loop.
func TestEnsureFrpcBinaryReacquiresWhenMissing(t *testing.T) {
	m := newCapabilityTestManager(t)
	frpcPath := filepath.Join(m.config.CommandLineArguments.AgentDir, "frpc")

	called := false
	m.SetReacquireFrpc(func() error {
		called = true
		return os.WriteFile(frpcPath, []byte("binary"), 0755)
	})

	require.NoError(t, m.ensureFrpcBinary())
	assert.True(t, called, "re-acquire must run when the binary is missing")
	assert.FileExists(t, frpcPath)
}

func TestEnsureFrpcBinaryNoopWhenPresent(t *testing.T) {
	m := newCapabilityTestManager(t)
	frpcPath := filepath.Join(m.config.CommandLineArguments.AgentDir, "frpc")
	require.NoError(t, os.WriteFile(frpcPath, []byte("binary"), 0755))

	m.SetReacquireFrpc(func() error {
		t.Fatal("re-acquire must not run when the binary is present")
		return nil
	})

	require.NoError(t, m.ensureFrpcBinary())
}

func TestEnsureFrpcBinaryFailsWhenReacquireFails(t *testing.T) {
	m := newCapabilityTestManager(t)
	m.SetReacquireFrpc(func() error { return errors.New("download blocked") })

	err := m.ensureFrpcBinary()
	require.ErrorContains(t, err, "re-acquire failed")
}

func TestEnsureFrpcBinaryFailsWithoutReacquire(t *testing.T) {
	m := newCapabilityTestManager(t)
	// no SetReacquireFrpc

	err := m.ensureFrpcBinary()
	require.ErrorContains(t, err, "no re-acquire available")
}

// Only one supervisor loop may run at a time: competing loops spawned frpc
// against frpc, and the loser's "bind: address already in use" exit marked
// tunnels unavailable even while a healthy client was up.
func TestSuperviseStartSingleFlight(t *testing.T) {
	m := newCapabilityTestManager(t)
	require.True(t, m.supervising.CompareAndSwap(false, true), "latching the supervisor flag must succeed on a fresh manager")
	defer m.supervising.Store(false)

	done := make(chan struct{})
	go func() {
		m.SuperviseStart()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SuperviseStart must return immediately while another supervisor is running")
	}

	cap, _ := m.Capability()
	assert.Equal(t, CapabilityUnknown, cap, "a skipped supervisor must not touch capability")
}

// Stop before any client was ever spawned must be a no-op, not a nil deref.
func TestStopWithoutClientIsSafe(t *testing.T) {
	m := newCapabilityTestManager(t)
	assert.NoError(t, m.Stop())
}

// reachableFrps points the manager at a live local listener, standing in for a
// running frps control port.
func reachableFrps(t *testing.T, m *FrpTunnelManager) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	m.configBuilder.yamlConfig.ServerAddr = "127.0.0.1"
	m.configBuilder.yamlConfig.ServerPort = ln.Addr().(*net.TCPAddr).Port
}

// unreachableFrps points the manager at a port nothing is listening on, standing
// in for the appliance tunnel profile being switched off.
func unreachableFrps(t *testing.T, m *FrpTunnelManager) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close()) // hand the port back before anyone can use it

	m.configBuilder.yamlConfig.ServerAddr = "127.0.0.1"
	m.configBuilder.yamlConfig.ServerPort = port
}

// The capability verdict comes from dialling frps, because frpc logs to a file
// and never tells us on stdout that it logged in.
func TestProbeCapabilityFollowsFrpsReachability(t *testing.T) {
	t.Run("reachable frps is capable", func(t *testing.T) {
		m := newCapabilityTestManager(t)
		reachableFrps(t, m)
		m.clientAlive.Store(true)

		m.probeCapability()

		cap, _ := m.Capability()
		assert.Equal(t, CapabilityAvailable, cap)
		assert.True(t, m.TunnelCapable())
	})

	t.Run("unreachable frps is not capable", func(t *testing.T) {
		m := newCapabilityTestManager(t)
		unreachableFrps(t, m)
		m.clientAlive.Store(true)

		m.probeCapability()

		cap, lastErr := m.Capability()
		assert.Equal(t, CapabilityUnavailable, cap)
		assert.False(t, m.TunnelCapable())
		assert.Contains(t, lastErr, "frps unreachable")
	})
}

// A start failure (missing/quarantined frpc) owns the capability. A frps that
// merely happens to be reachable must not paper over it.
func TestProbeCapabilityIgnoredWithoutRunningClient(t *testing.T) {
	m := newCapabilityTestManager(t)
	reachableFrps(t, m)
	m.MarkUnavailable("frpc binary quarantined")

	m.probeCapability() // clientAlive is false

	cap, lastErr := m.Capability()
	assert.Equal(t, CapabilityUnavailable, cap, "a reachable frps must not revive a device whose frpc cannot run")
	assert.Equal(t, "frpc binary quarantined", lastErr)
}

// Regression (found in production): frpc logs to /var/log/frpc.log, so
// "login to server success" never reaches the stdout the agent scans and the
// login ack never arrives. Treating that silence as failure marked every healthy
// device "tunnel disabled". A working tunnel must stay capable.
func TestStartStaysCapableWhenLoginAckNeverArrivesButFrpsIsUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake frpc is a shell script")
	}

	m := newCapabilityTestManager(t)
	m.loginDeadline = 200 * time.Millisecond
	reachableFrps(t, m)

	// frpc that runs happily but says nothing on stdout — the production case.
	fake := "#!/bin/sh\nsleep 2\n"
	frpcPath := filepath.Join(m.config.CommandLineArguments.AgentDir, "frpc")
	require.NoError(t, os.WriteFile(frpcPath, []byte(fake), 0o755))
	t.Cleanup(func() { _ = m.Stop() })

	require.NoError(t, m.Start())

	cap, _ := m.Capability()
	assert.Equal(t, CapabilityAvailable, cap, "a silent frpc against a reachable frps must NOT be reported unavailable")
	assert.True(t, m.TunnelCapable())
}

// The appliance case the badge exists for: frpc is up and retrying (loginFailExit
// = false keeps it alive), but the tunnel service is off, so nothing accepts a
// connection and the device must report itself unavailable.
func TestStartReportsUnavailableWhenFrpsIsDown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake frpc is a shell script")
	}

	m := newCapabilityTestManager(t)
	m.loginDeadline = 200 * time.Millisecond
	unreachableFrps(t, m)

	fake := "#!/bin/sh\necho '[E] connect to server error: connection refused'\nsleep 2\n"
	frpcPath := filepath.Join(m.config.CommandLineArguments.AgentDir, "frpc")
	require.NoError(t, os.WriteFile(frpcPath, []byte(fake), 0o755))
	t.Cleanup(func() { _ = m.Stop() })

	require.NoError(t, m.Start(), "Start must succeed and leave frpc retrying, not error out")

	cap, _ := m.Capability()
	assert.Equal(t, CapabilityUnavailable, cap)
	assert.False(t, m.TunnelCapable())
}

// The verdict is never permanent: once frps comes up, the next probe clears it.
// This is what saves an appliance where the agent wins the boot race against the
// frps container.
func TestUnavailableRecoversOnceFrpsComesUp(t *testing.T) {
	m := newCapabilityTestManager(t)
	unreachableFrps(t, m)
	m.clientAlive.Store(true)

	m.probeCapability()
	cap, _ := m.Capability()
	require.Equal(t, CapabilityUnavailable, cap)

	// frps starts listening on the very port that was refused a moment ago.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", m.configBuilder.yamlConfig.ServerPort))
	require.NoError(t, err)
	defer ln.Close()

	m.probeCapability()

	cap, lastErr := m.Capability()
	assert.Equal(t, CapabilityAvailable, cap, "a recovered frps must clear the badge without an agent restart")
	assert.Empty(t, lastErr)
}

// End-to-end reproduction of the production failure with a real spawned
// process: the admin port is taken by another socket (in the field: any
// outbound localhost connection, a stale frpc, a Docker publish), and frpc —
// like frp 0.69 when its webServer cannot bind — prints a bare
// "bind: address already in use" and exits before logging in. The supervisor
// must then persist a *different* admin port before the next attempt;
// retrying the dead port forever was what kept appliance tunnels down until a
// manual agent restart.
func TestSuperviseStartRepicksAdminPortAfterPreLoginExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake frpc is a shell script")
	}

	m := newCapabilityTestManager(t)

	fake := "#!/bin/sh\necho 'listen tcp 127.0.0.1:42567: bind: address already in use'\nexit 1\n"
	frpcPath := filepath.Join(m.config.CommandLineArguments.AgentDir, "frpc")
	require.NoError(t, os.WriteFile(frpcPath, []byte(fake), 0o755))

	initialPort, err := m.configBuilder.GetAdminPort()
	require.NoError(t, err)

	// Occupy the picked admin port, exactly like the socket that breaks frpc
	// in the field — the re-pick must not hand it out again.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", initialPort))
	require.NoError(t, err)
	defer ln.Close()

	go m.SuperviseStart()

	require.Eventually(t, func() bool {
		raw, readErr := os.ReadFile(m.configBuilder.ConfigPath)
		if readErr != nil {
			return false
		}
		var onDisk FrpcYamlConfig
		if yaml.Unmarshal(raw, &onDisk) != nil || onDisk.WebServer == nil {
			return false
		}
		return onDisk.WebServer.Port != initialPort && onDisk.WebServer.Port >= adminPortScanStart
	}, 10*time.Second, 100*time.Millisecond,
		"supervisor must persist a re-picked admin port after a pre-login exit")
}
