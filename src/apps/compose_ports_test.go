package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseComposePortEntry(t *testing.T) {
	cases := []struct {
		name          string
		raw           interface{}
		hostIP        string
		hostPort      uint64
		containerPort uint64
		protocol      string
		rewritable    bool
	}{
		{name: "host:container", raw: "8080:80", hostPort: 8080, containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "host:container tcp suffix", raw: "8080:80/tcp", hostPort: 8080, containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "host:container udp suffix", raw: "53:53/udp", hostPort: 53, containerPort: 53, protocol: "udp", rewritable: true},
		{name: "ip:host:container", raw: "127.0.0.1:8080:80", hostIP: "127.0.0.1", hostPort: 8080, containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "wildcard ip", raw: "0.0.0.0:8080:80", hostIP: "0.0.0.0", hostPort: 8080, containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "ipv6 host ip", raw: "[::1]:8080:80", hostIP: "[::1]", hostPort: 8080, containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "container only string", raw: "80", containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "container only number", raw: float64(80), containerPort: 80, protocol: "tcp", rewritable: true},
		{name: "range left alone", raw: "8080-8090:80-90", rewritable: false},
		{name: "container range left alone", raw: "8080-8090", rewritable: false},
		{name: "variable left alone", raw: "${WEB_PORT}:80", rewritable: false},
		{name: "bare variable left alone", raw: "${WEB_PORT}", rewritable: false},
		{name: "unknown protocol left alone", raw: "8080:80/sctp", rewritable: false},
		{
			name:          "long syntax",
			raw:           map[string]interface{}{"target": float64(80), "published": float64(8080), "protocol": "tcp"},
			hostPort:      8080,
			containerPort: 80,
			protocol:      "tcp",
			rewritable:    true,
		},
		{
			name:          "long syntax without published",
			raw:           map[string]interface{}{"target": float64(80)},
			containerPort: 80,
			protocol:      "tcp",
			rewritable:    true,
		},
		{
			name:       "long syntax published range left alone",
			raw:        map[string]interface{}{"target": float64(80), "published": "8000-9000"},
			protocol:   "tcp",
			rewritable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := parseComposePortEntry(tc.raw)
			assert.Equal(t, tc.rewritable, entry.Rewritable, "rewritable")
			if !tc.rewritable {
				return
			}
			assert.Equal(t, tc.hostIP, entry.HostIP, "host ip")
			assert.Equal(t, tc.hostPort, entry.HostPort, "host port")
			assert.Equal(t, tc.containerPort, entry.ContainerPort, "container port")
			assert.Equal(t, tc.protocol, entry.Protocol, "protocol")
		})
	}
}

func TestComposePortEntryDeclaredPort(t *testing.T) {
	published := parseComposePortEntry("8080:80")
	assert.Equal(t, uint64(8080), published.DeclaredPort(), "authored host port is the declared identity")

	containerOnly := parseComposePortEntry("80")
	assert.Equal(t, uint64(80), containerOnly.DeclaredPort())
}

func TestComposePortEntryMatchesRule(t *testing.T) {
	entry := parseComposePortEntry("8080:80")

	assert.True(t, entry.matchesRule(8080, "http"), "http rules ride tcp entries")
	assert.True(t, entry.matchesRule(8080, "tcp"))
	assert.False(t, entry.matchesRule(80, "http"), "rules match the authored host port, not the container port")
	assert.False(t, entry.matchesRule(8080, "udp"))

	udpEntry := parseComposePortEntry("5000:5000/udp")
	assert.True(t, udpEntry.matchesRule(5000, "udp"))
	assert.False(t, udpEntry.matchesRule(5000, "http"))
}

func TestRewriteComposePortEntry(t *testing.T) {
	shortSyntax := parseComposePortEntry("8080:80")
	assert.Equal(t, "0.0.0.0:41234:80", rewriteComposePortEntry(shortSyntax, "0.0.0.0", 41234))

	udp := parseComposePortEntry("53:53/udp")
	assert.Equal(t, "0.0.0.0:41235:53/udp", rewriteComposePortEntry(udp, "0.0.0.0", 41235))

	containerOnly := parseComposePortEntry("80")
	assert.Equal(t, "0.0.0.0:41236:80", rewriteComposePortEntry(containerOnly, "0.0.0.0", 41236))

	longSyntax := parseComposePortEntry(map[string]interface{}{"target": float64(80), "published": float64(8080)})
	rewritten, ok := rewriteComposePortEntry(longSyntax, "0.0.0.0", 41237).(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "41237", rewritten["published"])
	assert.Equal(t, "0.0.0.0", rewritten["host_ip"])
	assert.Equal(t, float64(80), rewritten["target"])
	// The original long-syntax map is not mutated.
	original := longSyntax.Raw.(map[string]interface{})
	assert.Equal(t, float64(8080), original["published"])
}

func TestParseComposePortsWalksAllServices(t *testing.T) {
	compose := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"ports": []interface{}{"8080:80", "9090:90"},
			},
			"metrics": map[string]interface{}{
				"ports": []interface{}{float64(9100)},
			},
			"worker": map[string]interface{}{}, // no ports
		},
	}

	entries := parseComposePorts(compose)
	assert.Len(t, entries, 3)

	byService := map[string]int{}
	for _, entry := range entries {
		byService[entry.Service]++
	}
	assert.Equal(t, 2, byService["web"])
	assert.Equal(t, 1, byService["metrics"])
}
