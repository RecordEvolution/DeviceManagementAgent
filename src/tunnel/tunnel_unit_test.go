// Unit tests for the frps-free pure helpers in the tunnel package. These do not
// start frpc and need no live frps server, so they run under `just test` (no
// build tag). The frps-dependent end-to-end test lives in tunnel_test.go
// behind //go:build integration.
package tunnel

import (
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// builderConfig returns a config whose AgentDir points at a writable temp dir
// so that initialize()/SaveConfig() can persist frpc.yaml without touching a
// fixed path. Only the fields the config builder reads are populated.
func builderConfig(t *testing.T, reswarm *config.ReswarmConfig) *config.Config {
	t.Helper()
	return &config.Config{
		CommandLineArguments: &config.CommandLineArguments{AgentDir: t.TempDir()},
		ReswarmConfig:        reswarm,
	}
}

func TestCreateTunnelID(t *testing.T) {
	tests := []struct {
		name      string
		subdomain string
		protocol  string
		expected  string
	}{
		{name: "http", subdomain: "9-myapp-8080", protocol: "http", expected: "9-myapp-8080-http"},
		{name: "tcp", subdomain: "9-myapp-22", protocol: "tcp", expected: "9-myapp-22-tcp"},
		{name: "empty subdomain", subdomain: "", protocol: "udp", expected: "-udp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CreateTunnelID(tt.subdomain, tt.protocol))
		})
	}
}

func TestCreateSubdomain(t *testing.T) {
	tests := []struct {
		name      string
		protocol  Protocol
		deviceKey uint64
		appName   string
		localPort uint64
		expected  string
	}{
		{
			name:      "http lowercases app name",
			protocol:  HTTP,
			deviceKey: 9,
			appName:   "MyApp",
			localPort: 8080,
			expected:  "9-myapp-8080",
		},
		{
			name:      "tcp keeps base form",
			protocol:  TCP,
			deviceKey: 42,
			appName:   "svc",
			localPort: 22,
			expected:  "42-svc-22",
		},
		{
			name:      "https gets secure prefix",
			protocol:  HTTPS,
			deviceKey: 7,
			appName:   "Secure-App",
			localPort: 443,
			expected:  "secure-7-secure-app-443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateSubdomain(tt.protocol, tt.deviceKey, tt.appName, tt.localPort)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// CreateSubdomain output fed into CreateTunnelID must round-trip the app name
// back out via the subdomainRegex used by GetTunnelConfig.
func TestCreateSubdomainParsableByGetTunnelConfig(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	subdomain := CreateSubdomain(HTTP, 9, "MyApp", 8080)
	builder.AddTunnelConfig(TunnelConfig{
		Subdomain: subdomain,
		Protocol:  HTTP,
		LocalPort: 8080,
	})

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "myapp", configs[0].AppName)
}

func TestProtocolConstants(t *testing.T) {
	assert.Equal(t, Protocol("tcp"), TCP)
	assert.Equal(t, Protocol("udp"), UDP)
	assert.Equal(t, Protocol("http"), HTTP)
	assert.Equal(t, Protocol("https"), HTTPS)
}

func TestBuildURL(t *testing.T) {
	// buildURL only reads configBuilder.BaseTunnelURL, so we can construct the
	// manager directly with a hand-built builder — no frpc process involved.
	frpTm := &FrpTunnelManager{
		configBuilder: TunnelConfigBuilder{BaseTunnelURL: "app.ironflock.com"},
	}

	tests := []struct {
		name       string
		protocol   Protocol
		subdomain  string
		remotePort uint64
		expected   string
	}{
		{
			name:      "http always upgrades to https without port",
			protocol:  HTTP,
			subdomain: "9-myapp-8080",
			expected:  "https://9-myapp-8080.app.ironflock.com",
		},
		{
			name:      "https keeps scheme without port",
			protocol:  HTTPS,
			subdomain: "secure-9-myapp-443",
			expected:  "https://secure-9-myapp-443.app.ironflock.com",
		},
		{
			name:       "http ignores remote port",
			protocol:   HTTP,
			subdomain:  "9-myapp-8080",
			remotePort: 12345,
			expected:   "https://9-myapp-8080.app.ironflock.com",
		},
		{
			name:       "tcp includes remote port",
			protocol:   TCP,
			subdomain:  "9-svc-22",
			remotePort: 30022,
			expected:   "tcp://9-svc-22.app.ironflock.com:30022",
		},
		{
			name:      "tcp without remote port omits port",
			protocol:  TCP,
			subdomain: "9-svc-22",
			expected:  "tcp://9-svc-22.app.ironflock.com",
		},
		{
			name:       "udp includes remote port",
			protocol:   UDP,
			subdomain:  "9-svc-53",
			remotePort: 30053,
			expected:   "udp://9-svc-53.app.ironflock.com:30053",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := frpTm.buildURL(tt.protocol, tt.subdomain, tt.remotePort)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestInterfaceToPortForwardRule(t *testing.T) {
	t.Run("defaults empty protocol to http", func(t *testing.T) {
		dat := []interface{}{
			map[string]interface{}{
				"name": "web",
				"port": float64(8080),
			},
		}

		rules, err := InterfaceToPortForwardRule(dat)
		require.NoError(t, err)
		require.Len(t, rules, 1)
		assert.Equal(t, "web", rules[0].RuleName)
		assert.Equal(t, uint64(8080), rules[0].Port)
		assert.Equal(t, "http", rules[0].Protocol)
	})

	t.Run("preserves explicit protocol and flags", func(t *testing.T) {
		dat := []interface{}{
			map[string]interface{}{
				"name":        "ssh",
				"port":        float64(22),
				"protocol":    "tcp",
				"main":        true,
				"public":      true,
				"remote_port": float64(30022),
			},
		}

		rules, err := InterfaceToPortForwardRule(dat)
		require.NoError(t, err)
		require.Len(t, rules, 1)
		assert.Equal(t, "tcp", rules[0].Protocol)
		assert.Equal(t, uint64(30022), rules[0].RemotePort)
		assert.True(t, rules[0].Main)
		assert.True(t, rules[0].Public)
	})

	t.Run("empty input yields empty slice", func(t *testing.T) {
		rules, err := InterfaceToPortForwardRule([]interface{}{})
		require.NoError(t, err)
		assert.Empty(t, rules)
	})
}

func TestPortForwardRuleToInterfaceRoundTrip(t *testing.T) {
	original := []common.PortForwardRule{
		{RuleName: "web", Port: 8080, Protocol: "http", Public: true},
		{RuleName: "ssh", Port: 22, Protocol: "tcp", RemotePort: 30022, Main: true},
	}

	ifaces, err := PortForwardRuleToInterface(original)
	require.NoError(t, err)
	require.Len(t, ifaces, 2)

	roundTripped, err := InterfaceToPortForwardRule(ifaces)
	require.NoError(t, err)
	require.Len(t, roundTripped, 2)
	assert.Equal(t, original, roundTripped)
}

func TestParseProxyStatus(t *testing.T) {
	t.Run("parses status and extracts remote port", func(t *testing.T) {
		text := `{"tcp":[{"name":"9-svc-22-tcp","type":"tcp","status":"running","err":"","local_addr":"127.0.0.1:22","plugin":"","remote_addr":"app.ironflock.com:30022"}]}`

		statuses, err := parseProxyStatus(text)
		require.NoError(t, err)
		require.Len(t, statuses, 1)

		s := statuses[0]
		assert.Equal(t, "9-svc-22-tcp", s.Name)
		assert.Equal(t, "running", s.Status)
		assert.Equal(t, Protocol("tcp"), s.Protocol)
		assert.Equal(t, "127.0.0.1:22", s.LocalAddr)
		assert.Equal(t, uint64(30022), s.RemotePort)
		assert.Empty(t, s.Error)
	})

	t.Run("carries error message through", func(t *testing.T) {
		text := `{"http":[{"name":"9-web-8080-http","type":"http","status":"error","err":"port already used","local_addr":"127.0.0.1:8080","remote_addr":""}]}`

		statuses, err := parseProxyStatus(text)
		require.NoError(t, err)
		require.Len(t, statuses, 1)

		s := statuses[0]
		assert.Equal(t, "port already used", s.Error)
		assert.Equal(t, uint64(0), s.RemotePort)
	})

	t.Run("empty map yields empty slice", func(t *testing.T) {
		statuses, err := parseProxyStatus(`{}`)
		require.NoError(t, err)
		assert.Empty(t, statuses)
	})

	t.Run("malformed remote port is an error", func(t *testing.T) {
		text := `{"tcp":[{"name":"x","type":"tcp","status":"running","remote_addr":"host:notaport"}]}`

		_, err := parseProxyStatus(text)
		assert.Error(t, err)
	})

	t.Run("invalid json is an error", func(t *testing.T) {
		_, err := parseProxyStatus(`not-json`)
		assert.Error(t, err)
	})
}

// initialize() resolves the frps server address (and thus BaseTunnelURL) from
// the ReswarmConfig with a fixed precedence: appliance_domain >
// device_endpoint_url (subdomain rewritten to "app") > environment default.
func TestServerAddressResolution(t *testing.T) {
	tests := []struct {
		name     string
		reswarm  *config.ReswarmConfig
		expected string
	}{
		{
			name:     "production default",
			reswarm:  &config.ReswarmConfig{Environment: string(common.PRODUCTION)},
			expected: PROD_SERVER_ADDR,
		},
		{
			name:     "test default",
			reswarm:  &config.ReswarmConfig{Environment: string(common.TEST)},
			expected: TEST_SERVER_ADDR,
		},
		{
			name:     "local default",
			reswarm:  &config.ReswarmConfig{Environment: string(common.LOCAL)},
			expected: "localhost",
		},
		{
			name:     "appliance_domain wins over everything",
			reswarm:  &config.ReswarmConfig{ApplianceDomain: "tunnel.appliance.example.com", DeviceEndpointURL: "https://api.ironflock.com", Environment: string(common.PRODUCTION)},
			expected: "tunnel.appliance.example.com",
		},
		{
			name:     "device_endpoint_url rewrites leading subdomain to app",
			reswarm:  &config.ReswarmConfig{DeviceEndpointURL: "https://api.ironflock.com", Environment: string(common.PRODUCTION)},
			expected: "app.ironflock.com",
		},
		{
			name:     "device_endpoint_url IP literal kept as-is",
			reswarm:  &config.ReswarmConfig{DeviceEndpointURL: "https://192.168.0.21:8080", Environment: string(common.PRODUCTION)},
			expected: "192.168.0.21",
		},
		{
			name:     "device_endpoint_url localhost kept as-is",
			reswarm:  &config.ReswarmConfig{DeviceEndpointURL: "http://localhost:3000", Environment: string(common.LOCAL)},
			expected: "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTunnelConfigBuilder(builderConfig(t, tt.reswarm))
			assert.Equal(t, tt.expected, builder.BaseTunnelURL)
		})
	}
}

func TestConfigBuilderAddAndGetTunnelConfig(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	httpConf := TunnelConfig{
		Subdomain: CreateSubdomain(HTTP, 9, "web", 8080),
		Protocol:  HTTP,
		LocalPort: 8080,
		LocalIP:   "127.0.0.1",
	}
	tcpConf := TunnelConfig{
		Subdomain:  CreateSubdomain(TCP, 9, "ssh", 22),
		Protocol:   TCP,
		LocalPort:  22,
		RemotePort: 30022,
	}

	builder.AddTunnelConfig(httpConf)
	builder.AddTunnelConfig(tcpConf)

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	require.Len(t, configs, 2)

	byProto := map[Protocol]TunnelConfig{}
	for _, c := range configs {
		byProto[c.Protocol] = c
	}

	// HTTP proxy keeps subdomain, no remote port.
	gotHTTP := byProto[HTTP]
	assert.Equal(t, httpConf.Subdomain, gotHTTP.Subdomain)
	assert.Equal(t, uint64(8080), gotHTTP.LocalPort)
	assert.Equal(t, "127.0.0.1", gotHTTP.LocalIP)
	assert.Equal(t, "web", gotHTTP.AppName)
	assert.Equal(t, uint64(0), gotHTTP.RemotePort)

	// TCP proxy carries remote port, has no subdomain field.
	gotTCP := byProto[TCP]
	assert.Equal(t, uint64(22), gotTCP.LocalPort)
	assert.Equal(t, uint64(30022), gotTCP.RemotePort)
	assert.Equal(t, "ssh", gotTCP.AppName)
	assert.Empty(t, gotTCP.Subdomain)
}

func TestConfigBuilderAddTunnelConfigDeduplicates(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	conf := TunnelConfig{
		Subdomain: CreateSubdomain(HTTP, 9, "web", 8080),
		Protocol:  HTTP,
		LocalPort: 8080,
	}

	builder.AddTunnelConfig(conf)
	builder.AddTunnelConfig(conf) // same Name -> should be skipped

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	assert.Len(t, configs, 1)
}

func TestConfigBuilderRemoveTunnelConfig(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	conf := TunnelConfig{
		Subdomain: CreateSubdomain(HTTP, 9, "web", 8080),
		Protocol:  HTTP,
		LocalPort: 8080,
	}

	builder.AddTunnelConfig(conf)
	require.Len(t, mustConfigs(t, &builder), 1)

	builder.RemoveTunnelConfig(conf)
	assert.Empty(t, mustConfigs(t, &builder))

	// Removing a non-existent tunnel is a no-op (does not panic / error).
	builder.RemoveTunnelConfig(TunnelConfig{Subdomain: "does-not-exist", Protocol: TCP})
	assert.Empty(t, mustConfigs(t, &builder))
}

func TestConfigBuilderSetTunnelVariable(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	conf := TunnelConfig{
		Subdomain:  CreateSubdomain(TCP, 9, "ssh", 22),
		Protocol:   TCP,
		LocalPort:  22,
		RemotePort: 30022,
	}
	builder.AddTunnelConfig(conf)

	tunnelID := CreateTunnelID(conf.Subdomain, string(conf.Protocol))
	builder.SetTunnelVariable(tunnelID, LOCAL_PORT, "2200")
	builder.SetTunnelVariable(tunnelID, REMOTE_PORT, "30099")
	builder.SetTunnelVariable(tunnelID, LOCAL_IP, "10.0.0.5")

	configs := mustConfigs(t, &builder)
	require.Len(t, configs, 1)
	assert.Equal(t, uint64(2200), configs[0].LocalPort)
	assert.Equal(t, uint64(30099), configs[0].RemotePort)
	assert.Equal(t, "10.0.0.5", configs[0].LocalIP)
}

func TestConfigBuilderSetCommonVariableAndAdminPort(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	builder.SetCommonVariable(SERVER_ADDRESS, "custom.example.com")
	builder.SetCommonVariable(SERVER_PORT, "7777")
	builder.SetCommonVariable(ADMIN_PORT, "48000")
	builder.SetCommonVariable(ENALBE_TLS, "false")

	port, err := builder.GetAdminPort()
	require.NoError(t, err)
	assert.Equal(t, 48000, port)
}

func TestGetAdminPortInitialised(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	// initialize() always wires a WebServer, so a port is available.
	port, err := builder.GetAdminPort()
	require.NoError(t, err)
	assert.Greater(t, port, 0)
}

// mustConfigs is a small assertion helper local to this file.
func mustConfigs(t *testing.T, builder *TunnelConfigBuilder) []TunnelConfig {
	t.Helper()
	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	return configs
}
