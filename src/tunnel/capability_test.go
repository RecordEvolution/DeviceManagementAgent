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
