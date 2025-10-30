package tunnel

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"regexp"
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
}

// YAML config structures matching frp v0.65.0 format
type FrpcYamlConfig struct {
	ServerAddr    string        `yaml:"serverAddr"`
	ServerPort    int           `yaml:"serverPort"`
	Transport     *Transport    `yaml:"transport,omitempty"`
	WebServer     *WebServer    `yaml:"webServer,omitempty"`
	Log           *LogConfig    `yaml:"log,omitempty"`
	LoginFailExit bool          `yaml:"loginFailExit,omitempty"`
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
	SubDomain  string          `yaml:"subDomain,omitempty"`
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

var subdomainRegex = regexp.MustCompile(`\d+-(.*)-\d+`)

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

	// Extract server address from device_endpoint_url
	serverAddr := PROD_SERVER_ADDR // Default fallback

	if cfg.ReswarmConfig.DeviceEndpointURL != "" {
		parsedURL, err := url.Parse(cfg.ReswarmConfig.DeviceEndpointURL)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to parse device_endpoint_url, using default: %s", serverAddr)
		} else {
			// Extract hostname (without port)
			hostname := parsedURL.Hostname()
			if hostname != "" {
				// For localhost, use as-is; otherwise replace subdomain with "app"
				if hostname == "localhost" || hostname == "127.0.0.1" {
					serverAddr = hostname
				} else {
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

	// Get admin port
	port := 7400
	randomPort, err := common.GetFreePortFromStart(30000)
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
			To:      "/var/log/frpc.log",
			Level:   "debug",
			MaxDays: 3,
		},
		LoginFailExit: false,
		Proxies:       []ProxyConfig{},
	}

	// For local development, use port 7400 to avoid conflicts with macOS services
	if serverAddr == "localhost" || serverAddr == "127.0.0.1" {
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
		tunnelConfig := TunnelConfig{
			Protocol:   Protocol(proxy.Type),
			LocalPort:  uint64(proxy.LocalPort),
			RemotePort: uint64(proxy.RemotePort),
			LocalIP:    proxy.LocalIP,
			Subdomain:  proxy.SubDomain,
		}

		var appName string
		result := subdomainRegex.FindStringSubmatch(proxy.SubDomain)

		if len(result) > 1 {
			appName = result[1]
		} else {
			log.Error().Msg("Failed to get app name from tunnel config")
		}

		tunnelConfig.AppName = appName
		tunnelConfigs = append(tunnelConfigs, tunnelConfig)
	}

	return tunnelConfigs, nil
}

func (builder *TunnelConfigBuilder) AddTunnelConfig(conf TunnelConfig) {
	tunnelID := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	proxyConfig := ProxyConfig{
		Name:      tunnelID,
		Type:      string(conf.Protocol),
		SubDomain: conf.Subdomain,
		LocalPort: int(conf.LocalPort),
	}

	if conf.LocalIP != "" {
		proxyConfig.LocalIP = conf.LocalIP
	}

	if conf.Protocol != HTTP && conf.Protocol != HTTPS {
		proxyConfig.RemotePort = int(conf.RemotePort)
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
	port := 7400
	randomPort, err := common.GetFreePortFromStart(30000)
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
