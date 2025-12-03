package testutil

import (
"reagent/common"
"reagent/config"
)

// DefaultTestConfig returns a minimal test configuration
func DefaultTestConfig() *config.Config {
	return &config.Config{
		CommandLineArguments: &config.CommandLineArguments{
			AgentDir:      "/opt/reagent",
			PrettyLogging: true,
			Debug:         true,
		},
		ReswarmConfig: &config.ReswarmConfig{
			Environment:       string(common.PRODUCTION),
			SwarmKey:          1,
			DeviceKey:         1,
			SerialNumber:      "test-serial-001",
			Secret:            "test-secret",
			DeviceEndpointURL: "ws://localhost:8080/ws",
			ReswarmBaseURL:    "https://app.ironflock.com",
		},
	}
}

// TestConfigBuilder provides a fluent interface for building test configs
type TestConfigBuilder struct {
	config *config.Config
}

// NewTestConfigBuilder creates a new builder with default values
func NewTestConfigBuilder() *TestConfigBuilder {
	return &TestConfigBuilder{
		config: DefaultTestConfig(),
	}
}

// WithSwarmKey sets the swarm key
func (b *TestConfigBuilder) WithSwarmKey(key int) *TestConfigBuilder {
	b.config.ReswarmConfig.SwarmKey = key
	return b
}

// WithDeviceKey sets the device key
func (b *TestConfigBuilder) WithDeviceKey(key int) *TestConfigBuilder {
	b.config.ReswarmConfig.DeviceKey = key
	return b
}

// WithSerialNumber sets the serial number
func (b *TestConfigBuilder) WithSerialNumber(serial string) *TestConfigBuilder {
	b.config.ReswarmConfig.SerialNumber = serial
	return b
}

// WithEnvironment sets the environment
func (b *TestConfigBuilder) WithEnvironment(env string) *TestConfigBuilder {
	b.config.ReswarmConfig.Environment = env
	return b
}

// WithEndpointURL sets the device endpoint URL
func (b *TestConfigBuilder) WithEndpointURL(url string) *TestConfigBuilder {
	b.config.ReswarmConfig.DeviceEndpointURL = url
	return b
}

// WithOfflineMode sets the offline mode flag
func (b *TestConfigBuilder) WithOfflineMode(offline bool) *TestConfigBuilder {
	b.config.CommandLineArguments.Offline = offline
	return b
}

// WithAgentDir sets the agent directory
func (b *TestConfigBuilder) WithAgentDir(dir string) *TestConfigBuilder {
	b.config.CommandLineArguments.AgentDir = dir
	return b
}

// WithDebug sets debug mode
func (b *TestConfigBuilder) WithDebug(debug bool) *TestConfigBuilder {
	b.config.CommandLineArguments.Debug = debug
	return b
}

// Build returns the configured config
func (b *TestConfigBuilder) Build() *config.Config {
	return b.config
}
