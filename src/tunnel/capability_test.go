package tunnel

import (
	"errors"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
