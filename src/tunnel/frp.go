package tunnel

import (
	"fmt"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"strings"

	"gopkg.in/ini.v1"
)

type Frp struct {
	Config      *config.Config
	wampSession *messenger.WampSession
}

type TunnelConfig struct {
	Subdomain  string
	Protocol   Protocol
	LocalPort  uint64
	RemotePort uint64
}

type TunnelConfigBuilder struct {
	ini           *ini.File
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
const REMOTE_PORT FrpcVariable = "remote_port"

func NewTunnelConfigBuilder(config *config.Config) TunnelConfigBuilder {
	frpcConfig := filepath.Join(config.CommandLineArguments.AgentDir, "frpc.ini")
	tunnelIni := ini.Empty()

	serverAddr := "app.datapods.io"
	switch config.ReswarmConfig.Environment {
	case string(common.PRODUCTION):
		serverAddr = "app.recordevolution.com"
	case string(common.TEST):
		serverAddr = "app.datapods.io"
	case string(common.LOCAL):
		serverAddr = "app.datapods.io"
	}

	configBuilder := TunnelConfigBuilder{
		ini:           tunnelIni,
		ConfigPath:    frpcConfig,
		BaseTunnelURL: serverAddr,
	}

	configBuilder.SetCommonVariable(SERVER_ADDRESS, serverAddr)
	configBuilder.SetCommonVariable(SERVER_PORT, "7000")
	configBuilder.SetCommonVariable(ENALBE_TLS, "true")
	configBuilder.SetCommonVariable(ADMIN_ADDRESS, "127.0.0.1")
	configBuilder.SetCommonVariable(ADMIN_PORT, "7400")

	configBuilder.SaveConfig()

	return configBuilder
}

func CreateTunnelID(subdomain string, protocol string) string {
	return fmt.Sprintf("%s-%s", subdomain, protocol)
}

func CreateSubdomain(deviceKey uint64, appName string, port uint64) string {
	return strings.ToLower(fmt.Sprintf("%d-%s-%d", deviceKey, appName, port))
}

func (builder *TunnelConfigBuilder) AddTunnelConfig(conf TunnelConfig) {
	tunnelID := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	builder.SetTunnelVariable(tunnelID, TYPE, string(conf.Protocol))
	builder.SetTunnelVariable(tunnelID, SUBDOMAIN, conf.Subdomain)
	builder.SetTunnelVariable(tunnelID, LOCAL_PORT, fmt.Sprintf("%d", conf.LocalPort))

	if conf.Protocol != HTTP {
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

func (builder *TunnelConfigBuilder) SetCommonVariable(key FrpcVariable, value string) {
	builder.ini.Section("common").Key(string(key)).SetValue(value)
}

func (builder *TunnelConfigBuilder) RemoveTunnelConfig(port TunnelConfig) {
	tunnelID := fmt.Sprintf("%s-%s-%d", port.Subdomain, port.Protocol, port.LocalPort)
	builder.RemoveTunnelVariable(tunnelID)

	builder.SaveConfig()
}

func (builder *TunnelConfigBuilder) SaveConfig() {
	builder.ini.SaveTo(builder.ConfigPath)
}
