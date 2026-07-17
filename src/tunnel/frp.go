package tunnel

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type Frp struct {
	Config      *config.Config
	wampSession *messenger.WampSession
}

type TunnelConfig struct {
	Subdomain  string
	AppName    string
	Protocol   Protocol
	LocalPort  uint64
	LocalIP    string
	RemotePort uint64
	// DeclaredPort is the app's declared port encoded in the subdomain. It is
	// the stable identity the UI matches tunnel state against; LocalPort is
	// merely the host port the agent published it on.
	DeclaredPort uint64
	// Name is the frpc proxy name, and the identity frpc reports status under.
	// Set by GetTunnelConfig when reading the config back; AddTunnel derives it
	// from Subdomain instead, so callers building a config to add need not set
	// it.
	Name string
}

// YAML config structures matching frp v0.65.0 format
type FrpcYamlConfig struct {
	ServerAddr    string        `yaml:"serverAddr"`
	ServerPort    int           `yaml:"serverPort"`
	Transport     *Transport    `yaml:"transport,omitempty"`
	WebServer     *WebServer    `yaml:"webServer,omitempty"`
	Log *LogConfig `yaml:"log,omitempty"`
	// Deliberately not omitempty: the value we want is false, which omitempty
	// drops — and frp defaults loginFailExit to true, so frpc would exit after
	// the first failed login instead of retrying. A frps that is briefly
	// unreachable would then take tunnels down until the agent restarts.
	LoginFailExit bool          `yaml:"loginFailExit"`
	Proxies       []ProxyConfig `yaml:"proxies,omitempty"`
}

type Transport struct {
	TLS *TLSConfig `yaml:"tls,omitempty"`
}

type TLSConfig struct {
	Enable bool `yaml:"enable"`
}

type WebServer struct {
	Addr string `yaml:"addr"`
	Port int    `yaml:"port"`
}

type LogConfig struct {
	To      string `yaml:"to"`
	Level   string `yaml:"level"`
	MaxDays int    `yaml:"maxDays"`
}

type ProxyConfig struct {
	Name       string          `yaml:"name"`
	Type       string          `yaml:"type"`
	LocalIP    string          `yaml:"localIP,omitempty"`
	LocalPort  int             `yaml:"localPort"`
	RemotePort int             `yaml:"remotePort,omitempty"`
	SubDomain  string          `yaml:"subdomain,omitempty"`
	Transport  *ProxyTransport `yaml:"transport,omitempty"`
}

type ProxyTransport struct {
	UseEncryption bool `yaml:"useEncryption,omitempty"`
}

type TunnelConfigBuilder struct {
	yamlConfig    *FrpcYamlConfig
	appConfig     *config.Config
	ConfigPath    string
	BaseTunnelURL string
}

type FrpcVariable string

const SERVER_ADDRESS FrpcVariable = "server_addr"
const SERVER_PORT FrpcVariable = "server_port"
const ENALBE_TLS FrpcVariable = "tls_enable"
const ADMIN_ADDRESS FrpcVariable = "admin_addr"
const ADMIN_PORT FrpcVariable = "admin_port"

const TYPE FrpcVariable = "type"
const SUBDOMAIN FrpcVariable = "subdomain"
const LOCAL_PORT FrpcVariable = "local_port"
const LOCAL_IP FrpcVariable = "local_ip"
const REMOTE_PORT FrpcVariable = "remote_port"

const PROD_SERVER_ADDR = "app.ironflock.com"
const TEST_SERVER_ADDR = "app.ironflock.dev"

// Matches "{deviceKey}-{appName}-{declaredPort}" (with an optional "secure-"
// prefix and, in TCP/UDP proxy names, a "-{protocol}" suffix — both ignored
// by the unanchored match). Group 1 is the app name, group 2 the declared
// port.
var subdomainRegex = regexp.MustCompile(`\d+-(.*)-(\d+)`)

func NewTunnelConfigBuilder(cfg *config.Config) TunnelConfigBuilder {
	return initialize(cfg)
}

func CreateTunnelID(subdomain string, protocol string) string {
	return fmt.Sprintf("%s-%s", subdomain, protocol)
}

func CreateSubdomain(protocol Protocol, deviceKey uint64, appName string, localPort uint64) string {
	baseSubdomain := strings.ToLower(fmt.Sprintf("%d-%s-%d", deviceKey, appName, localPort))
	if protocol == HTTPS {
		return fmt.Sprintf("%s-%s", "secure", baseSubdomain)
	}
	return baseSubdomain
}

func initialize(cfg *config.Config) TunnelConfigBuilder {
	frpcConfigPath := filepath.Join(cfg.CommandLineArguments.AgentDir, "frpc.yaml")

	// The default /var/log path is POSIX-only; on Windows it resolves to
	// C:\var\log whose parents don't exist, and frpc fails to open its log.
	// Keep Linux on the historical path (zero blast radius); put Windows under
	// the agent dir it already owns.
	frpcLogPath := "/var/log/frpc.log"
	if runtime.GOOS == "windows" {
		frpcLogPath = filepath.Join(cfg.CommandLineArguments.AgentDir, "frpc.log")
	}

	// Extract server address. Order of precedence:
	//   1. ReswarmConfig.ApplianceDomain (set on appliance installs from
	//      APPLIANCE_DOMAIN — the operator's tunnel domain, already correct).
	//   2. device_endpoint_url with the leading subdomain replaced by "app"
	//      (cloud case: api.ironflock.com -> app.ironflock.com). This rewrite
	//      is skipped when the hostname is an IP literal, since splitting on
	//      "." would mangle e.g. 192.168.0.21 into "app.168.0.21".
	//   3. Environment-based default.
	serverAddr := PROD_SERVER_ADDR // Default fallback

	if cfg.ReswarmConfig.ApplianceDomain != "" {
		serverAddr = cfg.ReswarmConfig.ApplianceDomain
		log.Debug().Msgf("Using tunnel server address from appliance_domain: %s", serverAddr)
	} else if cfg.ReswarmConfig.DeviceEndpointURL != "" {
		parsedURL, err := url.Parse(cfg.ReswarmConfig.DeviceEndpointURL)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to parse device_endpoint_url, using default: %s", serverAddr)
		} else {
			// Extract hostname (without port)
			hostname := parsedURL.Hostname()
			if hostname != "" {
				switch {
				case hostname == "localhost" || hostname == "127.0.0.1":
					serverAddr = hostname
				case hostname == "host.docker.internal":
					// The agent itself runs inside a container on a dev
					// machine; the name means "the machine hosting the dev
					// stack" (frps included) and must not be subdomain-
					// rewritten (app.docker.internal does not exist).
					serverAddr = hostname
				case net.ParseIP(hostname) != nil:
					// IP literal — no subdomain to replace; use as-is.
					serverAddr = hostname
				default:
					// Replace subdomain with "app"
					// e.g., "api.ironflock.com" -> "app.ironflock.com"
					parts := strings.Split(hostname, ".")
					if len(parts) >= 2 {
						parts[0] = "app"
						serverAddr = strings.Join(parts, ".")
					} else {
						serverAddr = hostname
					}
				}
				log.Debug().Msgf("Using tunnel server address from device_endpoint_url: %s", serverAddr)
			}
		}
	} else {
		// Fallback to environment-based configuration
		switch cfg.ReswarmConfig.Environment {
		case string(common.PRODUCTION):
			serverAddr = PROD_SERVER_ADDR
		case string(common.TEST):
			serverAddr = TEST_SERVER_ADDR
		case string(common.LOCAL):
			serverAddr = "localhost"
		}
		log.Debug().Msgf("Using tunnel server address from environment: %s", serverAddr)
	}

	// Get admin port. OS-assigned (port 0) rather than a scan from a fixed
	// base: scanning from 30000 landed exactly on the tunnel data-plane range
	// (TUNNEL_PORT_RANGE_START=30000) — with SO_REUSEADDR the loopback bind
	// succeeds alongside Docker's wildcard publish and silently shadows the
	// tunnel's host port for loopback traffic (or blocks the publish).
	port := 7400
	randomPort, err := common.GetRandomFreePort()
	if err == nil {
		port = randomPort
	}
	log.Debug().Msgf("Using port %d for Frp webserver", port)

	// Initialize YAML config structure
	frpcConfig := &FrpcYamlConfig{
		ServerAddr: serverAddr,
		ServerPort: 7000,
		Transport: &Transport{
			TLS: &TLSConfig{
				Enable: true,
			},
		},
		WebServer: &WebServer{
			Addr: "127.0.0.1",
			Port: port,
		},
		Log: &LogConfig{
			To:      frpcLogPath,
			Level:   "debug",
			MaxDays: 3,
		},
		LoginFailExit: false,
		Proxies:       []ProxyConfig{},
	}

	// For local development, use port 7400 to avoid conflicts with macOS
	// services. host.docker.internal is the same dev stack seen from inside a
	// containerized agent — the host publishes frps on 7400 there too.
	if serverAddr == "localhost" || serverAddr == "127.0.0.1" || serverAddr == "host.docker.internal" {
		frpcConfig.ServerPort = 7400
	}

	configBuilder := TunnelConfigBuilder{
		yamlConfig:    frpcConfig,
		appConfig:     cfg,
		ConfigPath:    frpcConfigPath,
		BaseTunnelURL: serverAddr,
	}

	configBuilder.SaveConfig()

	return configBuilder
}

func (builder *TunnelConfigBuilder) GetTunnelConfig() ([]TunnelConfig, error) {
	tunnelConfigs := make([]TunnelConfig, 0)

	for _, proxy := range builder.yamlConfig.Proxies {
		// Only HTTP/HTTPS proxies persist a subdomain (AddTunnelConfig writes
		// remotePort instead for TCP/UDP), so recover it from the proxy name —
		// which is CreateTunnelID(subdomain, protocol) — whenever it is absent.
		// Callers rebuild the tunnel id from Subdomain to match a config to its
		// frpc status: leaving it empty drops the tunnel from the reported
		// state entirely, and makes buildURL emit a host with an empty label.
		subdomain := proxy.SubDomain
		if subdomain == "" {
			subdomain = strings.TrimSuffix(proxy.Name, "-"+proxy.Type)
		}

		tunnelConfig := TunnelConfig{
			Name:         proxy.Name,
			Protocol:     Protocol(proxy.Type),
			LocalPort:    uint64(proxy.LocalPort),
			RemotePort:   uint64(proxy.RemotePort),
			LocalIP:      proxy.LocalIP,
			Subdomain:    subdomain,
			DeclaredPort: uint64(proxy.LocalPort),
		}

		result := subdomainRegex.FindStringSubmatch(subdomain)
		if len(result) > 2 {
			tunnelConfig.AppName = result[1]
			declaredPort, err := strconv.ParseUint(result[2], 10, 64)
			if err == nil {
				tunnelConfig.DeclaredPort = declaredPort
			}
		} else {
			log.Error().Str("subdomain", subdomain).Str("name", proxy.Name).Msg("Failed to parse app name from tunnel subdomain")
		}

		tunnelConfigs = append(tunnelConfigs, tunnelConfig)
	}

	return tunnelConfigs, nil
}

func (builder *TunnelConfigBuilder) AddTunnelConfig(conf TunnelConfig) {
	tunnelID := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	proxyConfig := ProxyConfig{
		Name:      tunnelID,
		Type:      string(conf.Protocol),
		LocalPort: int(conf.LocalPort),
	}

	if conf.LocalIP != "" {
		proxyConfig.LocalIP = conf.LocalIP
	}

	// subdomain is only valid for HTTP/HTTPS protocols
	if conf.Protocol == HTTP || conf.Protocol == HTTPS {
		proxyConfig.SubDomain = conf.Subdomain
	} else {
		// For TCP/UDP, use remotePort instead
		proxyConfig.RemotePort = int(conf.RemotePort)
	}

	// Upsert: a proxy left over from a previous agent run may point at a
	// stale local port (the app was republished on a different host port).
	// Skipping it would leave frpc dialing a dead port forever.
	for i, proxy := range builder.yamlConfig.Proxies {
		if proxy.Name != tunnelID {
			continue
		}

		if proxy.LocalPort == proxyConfig.LocalPort && proxy.LocalIP == proxyConfig.LocalIP {
			log.Debug().Str("tunnelID", tunnelID).Msg("Tunnel already exists in config, skipping add")
			return
		}

		log.Info().Str("tunnelID", tunnelID).Int("oldLocalPort", proxy.LocalPort).Int("newLocalPort", proxyConfig.LocalPort).Msg("Updating existing tunnel config")
		// Keep the remote port frps already granted this proxy unless the
		// caller supplies one; TCP/UDP proxies must not lose it on update.
		if proxyConfig.RemotePort == 0 {
			proxyConfig.RemotePort = proxy.RemotePort
		}
		builder.yamlConfig.Proxies[i] = proxyConfig
		builder.SaveConfig()
		return
	}

	builder.yamlConfig.Proxies = append(builder.yamlConfig.Proxies, proxyConfig)
	builder.SaveConfig()
}

func (builder *TunnelConfigBuilder) SetTunnelVariable(tunnelID string, key FrpcVariable, value string) {
	// Find and update existing proxy
	for i := range builder.yamlConfig.Proxies {
		if builder.yamlConfig.Proxies[i].Name == tunnelID {
			switch key {
			case TYPE:
				builder.yamlConfig.Proxies[i].Type = value
			case SUBDOMAIN:
				builder.yamlConfig.Proxies[i].SubDomain = value
			case LOCAL_PORT:
				var port int
				fmt.Sscanf(value, "%d", &port)
				builder.yamlConfig.Proxies[i].LocalPort = port
			case LOCAL_IP:
				builder.yamlConfig.Proxies[i].LocalIP = value
			case REMOTE_PORT:
				var port int
				fmt.Sscanf(value, "%d", &port)
				builder.yamlConfig.Proxies[i].RemotePort = port
			}
			return
		}
	}
}

func (builder *TunnelConfigBuilder) RemoveTunnelVariable(tunnelID string) {
	// Remove proxy with matching name
	newProxies := []ProxyConfig{}
	for _, proxy := range builder.yamlConfig.Proxies {
		if proxy.Name != tunnelID {
			newProxies = append(newProxies, proxy)
		}
	}
	builder.yamlConfig.Proxies = newProxies
}

func (builder *TunnelConfigBuilder) Reset() {
	*builder = initialize(builder.appConfig)
}

func (builder *TunnelConfigBuilder) SetAdminPort() {
	// OS-assigned, for the same reason as in initialize(): a scan from 30000
	// collides with the tunnel data-plane port range.
	port := 7400
	randomPort, err := common.GetRandomFreePort()
	if err == nil {
		port = randomPort
	}

	log.Debug().Msgf("Using port %d for Frp webserver", port)

	builder.yamlConfig.WebServer.Port = port
}

func (builder *TunnelConfigBuilder) SetCommonVariable(key FrpcVariable, value string) {
	switch key {
	case SERVER_ADDRESS:
		builder.yamlConfig.ServerAddr = value
	case SERVER_PORT:
		var port int
		fmt.Sscanf(value, "%d", &port)
		builder.yamlConfig.ServerPort = port
	case ENALBE_TLS:
		builder.yamlConfig.Transport.TLS.Enable = (value == "true")
	case ADMIN_ADDRESS:
		builder.yamlConfig.WebServer.Addr = value
	case ADMIN_PORT:
		var port int
		fmt.Sscanf(value, "%d", &port)
		builder.yamlConfig.WebServer.Port = port
	default:
		// Handle log and other settings
		if key == "log_file" {
			builder.yamlConfig.Log.To = value
		} else if key == "log_level" {
			builder.yamlConfig.Log.Level = value
		} else if key == "log_max_days" {
			var days int
			fmt.Sscanf(value, "%d", &days)
			builder.yamlConfig.Log.MaxDays = days
		}
	}
}

func (builder *TunnelConfigBuilder) GetAdminPort() (int, error) {
	if builder.yamlConfig.WebServer == nil {
		return 0, fmt.Errorf("webServer not configured")
	}
	return builder.yamlConfig.WebServer.Port, nil
}

func (builder *TunnelConfigBuilder) RemoveTunnelConfig(port TunnelConfig) {
	tunnelID := CreateTunnelID(port.Subdomain, string(port.Protocol))
	builder.RemoveTunnelVariable(tunnelID)
	builder.SaveConfig()
}

func (builder *TunnelConfigBuilder) SaveConfig() {
	data, err := yaml.Marshal(builder.yamlConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal frpc config to YAML")
		return
	}

	err = os.WriteFile(builder.ConfigPath, data, 0644)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to write frpc config to %s", builder.ConfigPath)
		return
	}

	log.Debug().Msgf("Saved frpc config to %s", builder.ConfigPath)
}
