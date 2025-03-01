package tunnel

import (
	"fmt"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/ini.v1"
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

type TunnelConfigBuilder struct {
	ini           *ini.File
	config        *config.Config
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
const TEST_SERVER_ADDR = "app.datapods.io"

func NewTunnelConfigBuilder(config *config.Config) TunnelConfigBuilder {
	return initialize(config)
}

func initialize(config *config.Config) TunnelConfigBuilder {
	frpcConfig := filepath.Join(config.CommandLineArguments.AgentDir, "frpc.ini")
	tunnelIni := ini.Empty()

	serverAddr := PROD_SERVER_ADDR

	switch config.ReswarmConfig.Environment {
	case string(common.PRODUCTION):
		serverAddr = PROD_SERVER_ADDR
	case string(common.TEST):
		serverAddr = TEST_SERVER_ADDR
	case string(common.LOCAL):
		serverAddr = TEST_SERVER_ADDR
	}

	configBuilder := TunnelConfigBuilder{
		ini:           tunnelIni,
		ConfigPath:    frpcConfig,
		BaseTunnelURL: serverAddr,
		config:        config,
	}

	configBuilder.SetCommonVariable(SERVER_ADDRESS, serverAddr)
	configBuilder.SetCommonVariable(SERVER_PORT, "7000")
	configBuilder.SetCommonVariable(ENALBE_TLS, "true")
	// configBuilder.SetCommonVariable(ADMIN_ADDRESS, "127.0.0.1")
	// configBuilder.SetAdminPort()

	configBuilder.SaveConfig()

	return configBuilder
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

var subdomainRegex = regexp.MustCompile(`\d+-(.*)-\d+`)

func (builder *TunnelConfigBuilder) GetTunnelConfig() ([]TunnelConfig, error) {
	tunnelConfigs := make([]TunnelConfig, 0)

	for _, section := range builder.ini.Sections() {
		name := section.Name()
		if name == "DEFAULT" || name == "common" {
			continue
		}

		tunnelConfig := TunnelConfig{}
		tunnelConfig.Protocol = Protocol(section.Key(string(TYPE)).String())

		localPort, err := section.Key(string(LOCAL_PORT)).Uint64()
		if err != nil {
			return nil, err
		}

		tunnelConfig.LocalPort = localPort

		// Remote port can be 0 if it's not set, do not handle error
		remotePort, _ := section.Key(string(REMOTE_PORT)).Uint64()

		tunnelConfig.RemotePort = remotePort

		tunnelConfig.LocalIP = section.Key(string(LOCAL_IP)).String()

		subdomain := section.Key(string(SUBDOMAIN)).String()
		tunnelConfig.Subdomain = subdomain

		var appName string
		result := subdomainRegex.FindStringSubmatch(subdomain)

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

	builder.SetTunnelVariable(tunnelID, TYPE, string(conf.Protocol))
	builder.SetTunnelVariable(tunnelID, SUBDOMAIN, conf.Subdomain)
	builder.SetTunnelVariable(tunnelID, LOCAL_PORT, fmt.Sprintf("%d", conf.LocalPort))

	if conf.LocalIP != "" {
		builder.SetTunnelVariable(tunnelID, LOCAL_IP, conf.LocalIP)
	}

	if conf.Protocol != HTTP && conf.Protocol != HTTPS {
		builder.SetTunnelVariable(tunnelID, REMOTE_PORT, fmt.Sprintf("%d", conf.RemotePort))
	}

	builder.SaveConfig()
}

func (builder *TunnelConfigBuilder) SetTunnelVariable(tunnelID string, key FrpcVariable, value string) {
	builder.ini.Section(tunnelID).Key(string(key)).SetValue(value)
}

func (builder *TunnelConfigBuilder) RemoveTunnelVariable(tunnelID string) {
	builder.ini.DeleteSection(tunnelID)

}

func (builder *TunnelConfigBuilder) Reset() {
	initialize(builder.config)
}

func (builder *TunnelConfigBuilder) SetAdminPort() {
	port := 7400
	randomPort, err := common.GetFreePortFromStart(30000)
	if err == nil {
		port = randomPort
	}

	log.Debug().Msgf("Using port %d for Frp webserver", port)

	builder.SetCommonVariable(ADMIN_PORT, fmt.Sprint(port))
}

func (builder *TunnelConfigBuilder) SetCommonVariable(key FrpcVariable, value string) {
	builder.ini.Section("common").Key(string(key)).SetValue(value)
}

func (builder *TunnelConfigBuilder) RemoveTunnelConfig(port TunnelConfig) {
	tunnelID := CreateTunnelID(port.Subdomain, string(port.Protocol))
	builder.RemoveTunnelVariable(tunnelID)

	builder.SaveConfig()
}

func (builder *TunnelConfigBuilder) SaveConfig() {
	builder.ini.SaveTo(builder.ConfigPath)
}
