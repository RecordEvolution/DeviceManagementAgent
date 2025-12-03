package messenger

import (
	"reagent/common"
	"reagent/config"
)

// testConfig returns a minimal test configuration for messenger tests
func testConfig() *config.Config {
	return &config.Config{
		CommandLineArguments: &config.CommandLineArguments{
			AgentDir:      "/opt/reagent",
			PrettyLogging: true,
			Debug:         true,
			Offline:       false,
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
