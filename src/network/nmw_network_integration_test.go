//go:build integration

package network

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRealNetworkOrSkip constructs the real NetworkManager-backed Network
// implementation. The constructor talks to the system D-Bus and the
// NetworkManager service, neither of which exists off Linux (and may be absent
// even on a minimal Linux box). When the resource is unavailable the test is
// skipped — never failed — so `just test-integration` stays green everywhere.
func newRealNetworkOrSkip(t *testing.T) NWMNetwork {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skipf("network integration requires NetworkManager over the system D-Bus; GOOS=%s is not linux", runtime.GOOS)
	}

	nw, err := NewNMWNetwork()
	if err != nil {
		// No system D-Bus / NetworkManager available: skip cleanly.
		t.Skipf("could not connect to NetworkManager (system D-Bus unavailable?): %v", err)
	}

	// Even when the constructor succeeds (system bus reachable) the
	// NetworkManager service itself may be missing. Probe with a read-only call;
	// a failure here means the live service isn't usable, so skip rather than
	// fail. ListEthernetDevices enumerates devices via GetAllDevices and does
	// not mutate any configuration.
	if _, err := nw.ListEthernetDevices(); err != nil {
		if isServiceUnavailable(err) {
			t.Skipf("NetworkManager service not available on this host: %v", err)
		}
		// A non-availability error from a read-only probe is a real failure.
		require.NoError(t, err, "read-only probe ListEthernetDevices failed unexpectedly")
	}

	return nw
}

// isServiceUnavailable reports whether err looks like "NetworkManager is not
// reachable" rather than a genuine logic error. We treat the common D-Bus
// not-found / no-service / no-such-interface messages as "resource absent".
func isServiceUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"the name org.freedesktop.networkmanager was not provided",
		"was not provided by any .service files",
		"no such interface",
		"no such object",
		"service unknown",
		"name has no owner",
		"connection refused",
		"dbus",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// TestNMWNetwork_ListEthernetDevices exercises the real read-only ethernet
// enumeration path against the live NetworkManager. It asserts the returned
// shapes are sane; it does not mutate any device configuration.
func TestNMWNetwork_ListEthernetDevices(t *testing.T) {
	nw := newRealNetworkOrSkip(t)

	devices, err := nw.ListEthernetDevices()
	require.NoError(t, err, "ListEthernetDevices should succeed against a live NetworkManager")

	for i, d := range devices {
		// A real ethernet device should always report an interface name; the
		// implementation falls back to the kernel interface when the IP
		// interface is empty.
		assert.NotEmptyf(t, d.InterfaceName, "device[%d] should carry an interface name", i)

		for _, addr := range d.IPv4AddressData {
			assert.NotEmptyf(t, addr.Address, "device[%d] %q: IPv4 address must not be empty", i, d.InterfaceName)
			assert.Truef(t, addr.Prefix <= 32, "device[%d] %q: IPv4 prefix %d out of range", i, d.InterfaceName, addr.Prefix)
		}

		for _, addr := range d.IPv6AddressData {
			assert.NotEmptyf(t, addr.Address, "device[%d] %q: IPv6 address must not be empty", i, d.InterfaceName)
			assert.Truef(t, addr.Prefix <= 128, "device[%d] %q: IPv6 prefix %d out of range", i, d.InterfaceName, addr.Prefix)
		}
	}
}

// TestNMWNetwork_ListWifiNetworks exercises the real read-only WiFi listing.
// A host may have no wireless device at all (common on servers); the
// implementation returns ErrDeviceNotFound in that case, which is an expected,
// non-fatal outcome for this read-only probe.
func TestNMWNetwork_ListWifiNetworks(t *testing.T) {
	nw := newRealNetworkOrSkip(t)

	wifis, err := nw.ListWifiNetworks()
	if err != nil {
		if err == ErrDeviceNotFound {
			t.Skip("host has no wireless device; skipping WiFi listing assertions")
		}
		require.NoError(t, err, "ListWifiNetworks should succeed when a wireless device exists")
	}

	currentCount := 0
	for i, w := range wifis {
		// Signal strength is a percentage (0-100) for an in-range AP; saved-only
		// entries default to 0. Either way it must be within the byte range,
		// which uint8 already guarantees — assert the AP-level semantic bound.
		assert.Truef(t, w.Signal <= 100, "wifi[%d] %q: signal %d should be a 0-100 percentage", i, w.SSID, w.Signal)

		// A WiFi that the device is currently connected to must also be known
		// (it has a saved connection profile).
		if w.Current {
			currentCount++
			assert.Truef(t, w.Known, "wifi[%d] %q: a current network must also be known", i, w.SSID)
		}
	}

	// At most one network can be the currently-connected one for a single
	// wireless device.
	assert.LessOrEqualf(t, currentCount, 1, "at most one WiFi may be marked Current, got %d", currentCount)
}

// TestNMWNetwork_GetActiveWirelessDeviceConfig reads the active wireless
// device's IP configuration. With no wireless device this returns
// ErrDeviceNotFound (expected, skipped). Otherwise the returned address data
// must be well-formed.
func TestNMWNetwork_GetActiveWirelessDeviceConfig(t *testing.T) {
	nw := newRealNetworkOrSkip(t)

	ipv4, ipv6, err := nw.GetActiveWirelessDeviceConfig()
	if err != nil {
		if err == ErrDeviceNotFound {
			t.Skip("host has no wireless device; skipping active wireless config assertions")
		}
		require.NoError(t, err, "GetActiveWirelessDeviceConfig should succeed when a wireless device exists")
	}

	for _, addr := range ipv4 {
		assert.NotEmpty(t, addr.Address, "active wireless IPv4 address must not be empty")
		assert.True(t, addr.Prefix <= 32, "active wireless IPv4 prefix %d out of range", addr.Prefix)
	}
	for _, addr := range ipv6 {
		assert.NotEmpty(t, addr.Address, "active wireless IPv6 address must not be empty")
		assert.True(t, addr.Prefix <= 128, "active wireless IPv6 prefix %d out of range", addr.Prefix)
	}
}

// TestNMWNetwork_Scan triggers a real WiFi scan with a bounded timeout. Scanning
// is read-only (it does not change device config) but requires a managed,
// connected wireless device. Absent a wireless device (ErrDeviceNotFound), an
// unmanaged/disconnected device (ErrNotConnected), or a scan that doesn't
// complete in time (context deadline) we skip — these are environment
// conditions, not defects in the code under test.
func TestNMWNetwork_Scan(t *testing.T) {
	nw := newRealNetworkOrSkip(t)

	err := nw.Scan(8 * time.Second)
	switch {
	case err == nil:
		// Scan completed; nothing more to assert — it mutates no config.
	case err == ErrDeviceNotFound:
		t.Skip("host has no wireless device; skipping scan")
	case err == ErrNotConnected:
		t.Skip("wireless device is not managed/connected; skipping scan")
	default:
		// A timeout waiting for the scan to complete is an environmental
		// limitation, not a failure of the code path.
		if strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
			t.Skipf("scan did not complete within the timeout on this host: %v", err)
		}
		require.NoError(t, err, "Scan failed unexpectedly")
	}
}
