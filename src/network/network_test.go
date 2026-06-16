package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapUint32 reverses the byte order of a 32-bit integer.
func TestSwapUint32(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{"zero", 0x00000000, 0x00000000},
		{"all ones", 0xFFFFFFFF, 0xFFFFFFFF},
		{"ascending bytes", 0x01020304, 0x04030201},
		{"low byte only", 0x000000FF, 0xFF000000},
		{"high byte only", 0xFF000000, 0x000000FF},
		{"middle bytes", 0x00AABB00, 0x00BBAA00},
		{"palindrome stays", 0x12344321, 0x21433412},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, swapUint32(tt.in))
		})
	}
}

// swapUint32 must be its own inverse: applying it twice yields the original.
func TestSwapUint32Involution(t *testing.T) {
	for _, v := range []uint32{0, 1, 0x12345678, 0xDEADBEEF, 0xFFFFFFFF, 0xC0A80001} {
		assert.Equal(t, v, swapUint32(swapUint32(v)),
			"swapUint32 applied twice should return the original value")
	}
}

// ip2Long converts a dotted IPv4 string into a host-order uint32. It first
// reads the 4 octets big-endian, then byte-swaps, so the result is the
// little-endian / host-order representation of the address.
func TestIP2Long(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want uint32
	}{
		// 1.2.3.4 -> big-endian read 0x01020304 -> swapped 0x04030201
		{"ascending octets", "1.2.3.4", 0x04030201},
		// 0.0.0.0
		{"zero address", "0.0.0.0", 0x00000000},
		// 255.255.255.255 -> 0xFFFFFFFF -> swapped 0xFFFFFFFF
		{"broadcast", "255.255.255.255", 0xFFFFFFFF},
		// 192.168.0.1 -> 0xC0A80001 -> swapped 0x0100A8C0
		{"private class C", "192.168.0.1", 0x0100A8C0},
		// 127.0.0.1 -> 0x7F000001 -> swapped 0x0100007F
		{"loopback", "127.0.0.1", 0x0100007F},
		// 8.8.8.8 -> 0x08080808 -> swapped 0x08080808 (palindromic)
		{"google dns", "8.8.8.8", 0x08080808},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ip2Long(tt.ip)
			assert.Equalf(t, tt.want, got, "ip2Long(%q) = %#08x, want %#08x", tt.ip, got, tt.want)
		})
	}
}

// ip2Long's swap of the big-endian read means it equals the host-order value of
// the reversed-octet address. Cross-check against an independent computation
// from net.ParseIP to guard against silent regressions in the conversion.
func TestIP2LongCrossCheck(t *testing.T) {
	for _, ip := range []string{"10.0.0.5", "172.16.254.1", "203.0.113.42"} {
		octets := net.ParseIP(ip).To4()
		require.NotNil(t, octets, "test input %q must be a valid IPv4", ip)

		// Independent reference: ip2Long reads octets big-endian then byte-swaps,
		// which yields octet[0] | octet[1]<<8 | octet[2]<<16 | octet[3]<<24.
		want := uint32(octets[0]) |
			uint32(octets[1])<<8 |
			uint32(octets[2])<<16 |
			uint32(octets[3])<<24

		assert.Equalf(t, want, ip2Long(ip), "mismatch for %q", ip)
	}
}

// The package-level ipv4regex (compiled from IPv4RegExp) drives the address
// filtering in GetIPv4Addresses. Verify its matching behavior directly.
func TestIPv4Regex(t *testing.T) {
	valid := []string{
		"0.0.0.0",
		"127.0.0.1",
		"192.168.1.1",
		"255.255.255.255",
		"10.0.0.1",
		"8.8.8.8",
		"249.249.249.249",
	}
	for _, ip := range valid {
		t.Run("valid/"+ip, func(t *testing.T) {
			assert.True(t, ipv4regex.MatchString(ip), "expected %q to match IPv4 regex", ip)
		})
	}

	invalid := []string{
		"",
		"256.0.0.1",       // octet out of range
		"1.2.3",           // too few octets
		"1.2.3.4.5",       // too many octets
		"1.2.3.04",        // trailing junk path still 4 octets but 04 is allowed; covered below
		"abc.def.ghi.jkl", // non-numeric
		"::1",             // IPv6
		"300.300.300.300", // all out of range
		"1.2.3.",          // trailing dot, missing octet
		".1.2.3",          // leading dot
		"1.2.3.4 ",        // trailing space
		"999.1.1.1",       // first octet out of range
	}
	for _, ip := range invalid {
		// "1.2.3.04" actually matches because [01]?[0-9][0-9]? permits a leading
		// zero; assert it matches rather than excluding it from the table.
		if ip == "1.2.3.04" {
			t.Run("valid-leading-zero/"+ip, func(t *testing.T) {
				assert.True(t, ipv4regex.MatchString(ip), "leading-zero octet should match")
			})
			continue
		}
		t.Run("invalid/"+ip, func(t *testing.T) {
			assert.False(t, ipv4regex.MatchString(ip), "expected %q NOT to match IPv4 regex", ip)
		})
	}
}

// GetIPv4Addresses enumerates host interfaces and applies the package's
// filtering rules. It must not error on a normal host, and every returned
// address must satisfy the public contract: a valid IPv4 string that is not a
// loopback/unspecified address and not on a docker interface.
func TestGetIPv4Addresses(t *testing.T) {
	addrs, err := GetIPv4Addresses()
	require.NoError(t, err)

	for _, a := range addrs {
		assert.NotEmpty(t, a.InterfaceName, "returned address should carry its interface name")
		assert.True(t, ipv4regex.MatchString(a.Ip), "returned IP %q must be a valid IPv4", a.Ip)
		assert.NotEqual(t, "127.0.0.1", a.Ip, "loopback should be filtered out")
		assert.NotEqual(t, "0.0.0.0", a.Ip, "unspecified address should be filtered out")
		assert.NotContains(t, a.InterfaceName, "docker", "docker interfaces should be filtered out")
	}
}
