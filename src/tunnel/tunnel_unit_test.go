// Unit tests for the frps-free pure helpers in the tunnel package. These do not
// start frpc and need no live frps server, so they run under `just test` (no
// build tag). The frps-dependent end-to-end test lives in tunnel_test.go
// behind //go:build integration.
package tunnel

import (
	"fmt"
	"net"
	"os"
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// frp defaults loginFailExit to true, so the intended false has to actually be
// written: frpc must retry a frps that is temporarily unreachable rather than
// exiting on the first failed login and taking tunnels down with it.
func TestConfigBuilderPersistsLoginFailExit(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	raw, err := os.ReadFile(builder.ConfigPath)
	require.NoError(t, err)

	assert.Contains(t, string(raw), "loginFailExit: false")
}

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
		{
			// Containerized dev agent (compose `agent` service): the name must
			// not be subdomain-rewritten into the nonexistent app.docker.internal.
			name:     "device_endpoint_url host.docker.internal kept as-is",
			reswarm:  &config.ReswarmConfig{DeviceEndpointURL: "ws://host.docker.internal:8080/ws-re-dev", Environment: string(common.LOCAL)},
			expected: "host.docker.internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewTunnelConfigBuilder(builderConfig(t, tt.reswarm))
			assert.Equal(t, tt.expected, builder.BaseTunnelURL)
		})
	}
}

// Local dev servers (native agent via localhost, containerized agent via
// host.docker.internal) expose frps on 7400; everything else uses 7000.
func TestServerPortResolution(t *testing.T) {
	local := NewTunnelConfigBuilder(builderConfig(t, &config.ReswarmConfig{DeviceEndpointURL: "ws://host.docker.internal:8080/ws-re-dev", Environment: string(common.LOCAL)}))
	assert.Equal(t, 7400, local.yamlConfig.ServerPort)

	prod := NewTunnelConfigBuilder(builderConfig(t, &config.ReswarmConfig{DeviceEndpointURL: "https://api.ironflock.com", Environment: string(common.PRODUCTION)}))
	assert.Equal(t, 7000, prod.yamlConfig.ServerPort)
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

	// TCP proxy carries a remote port. It stores no subdomain key, so the
	// subdomain is recovered from the proxy name — callers rebuild the tunnel
	// id from it to match the config against its frpc status.
	gotTCP := byProto[TCP]
	assert.Equal(t, uint64(22), gotTCP.LocalPort)
	assert.Equal(t, uint64(30022), gotTCP.RemotePort)
	assert.Equal(t, "ssh", gotTCP.AppName)
	assert.Equal(t, tcpConf.Subdomain, gotTCP.Subdomain)
	assert.Empty(t, builder.yamlConfig.Proxies[1].SubDomain, "no subdomain key is stored for tcp")
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

// frpc reads frpc.yaml, not the builder's memory: SetAdminPort must write the
// re-picked port to disk, or the recovery path respawns frpc on the very port
// that just failed with "bind: address already in use" — forever.
func TestSetAdminPortPersistsToDisk(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	// Divert the in-memory port without saving, so persistence (not a stale
	// file from initialize()) is what the assertion proves.
	builder.SetCommonVariable(ADMIN_PORT, "48000")

	builder.SetAdminPort()

	memPort, err := builder.GetAdminPort()
	require.NoError(t, err)
	assert.NotEqual(t, 48000, memPort, "SetAdminPort must re-pick the port")

	raw, err := os.ReadFile(builder.ConfigPath)
	require.NoError(t, err)

	var onDisk FrpcYamlConfig
	require.NoError(t, yaml.Unmarshal(raw, &onDisk))
	require.NotNil(t, onDisk.WebServer)
	assert.Equal(t, memPort, onDisk.WebServer.Port, "the re-picked admin port must be persisted for frpc to see")
}

// A port that is bound — by a stale frpc, an outbound connection, anything —
// must never be picked for the admin webserver again.
func TestPickAdminPortSkipsBoundPorts(t *testing.T) {
	first := pickAdminPort()
	require.GreaterOrEqual(t, first, adminPortScanStart)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", first))
	require.NoError(t, err)
	defer ln.Close()

	second := pickAdminPort()
	assert.NotEqual(t, first, second, "a bound port must not be re-picked")
	assert.GreaterOrEqual(t, second, adminPortScanStart)
}

// GetState matches a stored config to its frpc status by tunnel id. Only
// HTTP/HTTPS proxies persist a subdomain, so every protocol must still round-
// trip an id equal to the proxy name frpc reports — otherwise the tunnel is
// silently missing from the reported state and the UI shows it as never
// coming up.
func TestGetTunnelConfigRoundTripsTunnelIdentity(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	for _, tc := range []struct {
		protocol Protocol
		port     uint64
	}{
		{HTTP, 55000},
		{HTTPS, 55001},
		{TCP, 1883},
		{UDP, 51820},
	} {
		builder.AddTunnelConfig(TunnelConfig{
			Subdomain:  CreateSubdomain(tc.protocol, 4749, "trumpfqds", tc.port),
			Protocol:   tc.protocol,
			LocalPort:  40000 + tc.port,
			RemotePort: 30000,
		})
	}

	configs := mustConfigs(t, &builder)
	require.Len(t, configs, 4)

	for _, got := range configs {
		expectedName := CreateTunnelID(CreateSubdomain(got.Protocol, 4749, "trumpfqds", got.DeclaredPort), string(got.Protocol))

		// The name frpc reports status under, carried through verbatim.
		assert.Equal(t, expectedName, got.Name, "%s: proxy name must survive the round-trip", got.Protocol)
		// ...and still reconstructible from the subdomain, which TCP/UDP do
		// not persist and must recover from the name.
		assert.Equal(t, expectedName, CreateTunnelID(got.Subdomain, string(got.Protocol)),
			"%s: subdomain must rebuild the frpc proxy name", got.Protocol)
		assert.Equal(t, "trumpfqds", got.AppName, "%s: app name", got.Protocol)
	}
}

// A tunnel whose subdomain did not round-trip (e.g. a config written before
// the subdomain was persisted for its protocol) must still be identifiable
// from the proxy name alone.
func TestGetTunnelConfigRecoversSubdomainFromName(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	subdomain := CreateSubdomain(HTTP, 4749, "trumpfqds", 55000)
	builder.AddTunnelConfig(TunnelConfig{Subdomain: subdomain, Protocol: HTTP, LocalPort: 40003})

	// Simulate the subdomain key being absent from the stored proxy.
	builder.yamlConfig.Proxies[0].SubDomain = ""

	configs := mustConfigs(t, &builder)
	require.Len(t, configs, 1)

	assert.Equal(t, subdomain, configs[0].Subdomain, "subdomain must be recovered from the proxy name")
	assert.Equal(t, uint64(55000), configs[0].DeclaredPort)
	assert.Equal(t, "trumpfqds", configs[0].AppName)
}

// mustConfigs is a small assertion helper local to this file.
func mustConfigs(t *testing.T, builder *TunnelConfigBuilder) []TunnelConfig {
	t.Helper()
	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	return configs
}

// The subdomain encodes the app's DECLARED port; LocalPort is the (possibly
// different) agent-managed host port frpc dials. GetTunnelConfig must recover
// the declared port so TunnelState.Port keeps matching the UI's port entries.
func TestGetTunnelConfigParsesDeclaredPort(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	builder.AddTunnelConfig(TunnelConfig{
		Subdomain: CreateSubdomain(HTTP, 9, "web", 8080),
		Protocol:  HTTP,
		LocalPort: 41234, // managed host port, not the declared port
	})
	builder.AddTunnelConfig(TunnelConfig{
		Subdomain:  CreateSubdomain(TCP, 9, "ssh", 22),
		Protocol:   TCP,
		LocalPort:  41235,
		RemotePort: 30022,
	})

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	require.Len(t, configs, 2)

	byProto := map[Protocol]TunnelConfig{}
	for _, c := range configs {
		byProto[c.Protocol] = c
	}

	assert.Equal(t, uint64(8080), byProto[HTTP].DeclaredPort)
	assert.Equal(t, uint64(41234), byProto[HTTP].LocalPort)
	assert.Equal(t, uint64(22), byProto[TCP].DeclaredPort)
	assert.Equal(t, uint64(41235), byProto[TCP].LocalPort)
}

// The cloud is nudged to resync only when the fingerprint changes, so an
// unchanged read must fingerprint identically (no publish every 10s), and the
// order GetState happens to return proxies in must not look like a change.
func TestTunnelStateFingerprintStableAndOrderIndependent(t *testing.T) {
	a := TunnelState{Status: &TunnelStatus{Name: "9-web-8080-http", Status: "running"}, Active: true, URL: "https://a"}
	b := TunnelState{Status: &TunnelStatus{Name: "9-ssh-22-tcp", Status: "running", RemotePort: 30022}, Active: true, URL: "tcp://b"}

	first := tunnelStateFingerprint([]TunnelState{a, b})

	assert.Equal(t, first, tunnelStateFingerprint([]TunnelState{a, b}), "an identical re-read must not look like a change")
	assert.Equal(t, first, tunnelStateFingerprint([]TunnelState{b, a}), "ordering must not look like a change")
}

// Every field the UI renders a port square from has to move the fingerprint,
// or the cloud keeps showing a stale tunnel.
func TestTunnelStateFingerprintDetectsChanges(t *testing.T) {
	newState := func() []TunnelState {
		return []TunnelState{{
			Status: &TunnelStatus{Name: "9-ssh-22-tcp", Status: "running", RemotePort: 30022},
			Active: true,
		}}
	}
	baseline := tunnelStateFingerprint(newState())

	tests := []struct {
		name   string
		mutate func(s *TunnelState)
	}{
		{"proxy stopped running", func(s *TunnelState) { s.Status.Status = "error" }},
		{"remote port re-granted", func(s *TunnelState) { s.Status.RemotePort = 30099 }},
		{"went inactive", func(s *TunnelState) { s.Active = false }},
		{"error raised", func(s *TunnelState) { s.Error = true; s.ErrorMessage = "port already used" }},
		{"url changed", func(s *TunnelState) { s.URL = "tcp://new" }},
		{"proxy disappeared", func(s *TunnelState) { *s = TunnelState{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := newState()
			tt.mutate(&changed[0])
			assert.NotEqual(t, baseline, tunnelStateFingerprint(changed))
		})
	}
}

// A device with no proxies must fingerprint empty, so it never nudges the cloud
// about nothing.
func TestTunnelStateFingerprintEmptyWhenNoProxies(t *testing.T) {
	assert.Empty(t, tunnelStateFingerprint(nil))
	assert.Empty(t, tunnelStateFingerprint([]TunnelState{}))
}

// A configured proxy that frpc reports no status for arrives with a nil Status.
func TestTunnelStateFingerprintHandlesNilStatus(t *testing.T) {
	assert.NotPanics(t, func() {
		tunnelStateFingerprint([]TunnelState{{Active: false}})
	})
}

// A proxy left in frpc.yaml by a previous agent run may dial a stale host
// port; AddTunnelConfig must update it in place (and keep the granted remote
// port) instead of skipping.
func TestConfigBuilderAddTunnelConfigUpdatesStaleLocalPort(t *testing.T) {
	cfg := builderConfig(t, &config.ReswarmConfig{Environment: string(common.PRODUCTION)})
	builder := NewTunnelConfigBuilder(cfg)

	subdomain := CreateSubdomain(TCP, 9, "ssh", 22)
	builder.AddTunnelConfig(TunnelConfig{
		Subdomain:  subdomain,
		Protocol:   TCP,
		LocalPort:  41235,
		RemotePort: 30022,
	})

	// Same tunnel, new host port, no remote port supplied by the caller.
	builder.AddTunnelConfig(TunnelConfig{
		Subdomain: subdomain,
		Protocol:  TCP,
		LocalPort: 41999,
	})

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	require.Len(t, configs, 1, "update must not append a duplicate proxy")
	assert.Equal(t, uint64(41999), configs[0].LocalPort)
	assert.Equal(t, uint64(30022), configs[0].RemotePort, "granted remote port survives the update")
}
