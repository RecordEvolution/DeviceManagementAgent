package tunnel

import (
	"reagent/common"
	"reagent/config"
	"reagent/logging"
	"reagent/messenger"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func init() {
	logging.SetupLogger(&config.CommandLineArguments{PrettyLogging: true, Debug: true})
}

func setupTunnel() FrpTunnelManager {
	generalConfig := &config.Config{
		CommandLineArguments: &config.CommandLineArguments{AgentDir: "/opt/reagent"},
		ReswarmConfig:        &config.ReswarmConfig{Environment: string(common.PRODUCTION)},
	}

	dummyMessenger := messenger.NewOffline(generalConfig)
	tunnelManager, err := NewFrpTunnelManager(dummyMessenger, generalConfig)
	if err != nil {
		log.Fatal().Msg("Failed to initialize FrpTunnelManager")
	}

	err = tunnelManager.Start()
	if err != nil {
		log.Fatal().Msg("Failed to start FrpTunnelManager")
	}

	return tunnelManager
}

func TestAddTunnel(t *testing.T) {
	assert := assert.New(t)

	tunnelManager := setupTunnel()

	err := tunnelManager.Reset()
	if err != nil {
		t.Fatalf("Failed to reset tunnel %s", err.Error())
	}

	subdomain := CreateSubdomain(HTTP, 1, "Test", 8080)
	initialConfig := TunnelConfig{Protocol: HTTP, Subdomain: subdomain, LocalPort: 8080, RemotePort: 0}
	cfg, err := tunnelManager.AddTunnel(initialConfig)
	if err != nil {
		t.Fatalf("Failed to add tunnel %s", err.Error())
	}

	tunnelID := CreateTunnelID(cfg.Subdomain, string(cfg.Protocol))
	status, err := tunnelManager.Status(tunnelID)
	if err != nil {
		t.Fatalf("Failed get tunnel status %s", err.Error())
	}

	for {
		status, err = tunnelManager.Status(tunnelID)
		if err != nil {
			t.Fatalf("Failed get tunnel status %s", err.Error())
		}

		if status.Error != "" {
			t.Fatalf("Tunnel failed to start %s", err.Error())
		}

		if status.Status == "running" {
			break
		}
	}

	configs, err := tunnelManager.GetTunnelConfig()
	if err != nil {
		t.Fatalf("Failed to get tunnel config %s", err.Error())
	}

	config := configs[0]
	assert.Equal(config.LocalPort, initialConfig.LocalPort)
	assert.Equal(config.Protocol, initialConfig.Protocol)
	assert.Equal(config.RemotePort, initialConfig.RemotePort)
	assert.Equal(config.LocalIP, initialConfig.LocalIP)
	assert.Equal(config.Subdomain, initialConfig.Subdomain)

	err = tunnelManager.RemoveTunnel(cfg)
	if err != nil {
		t.Fatalf("Failed to remove tunnel %s", err.Error())
	}

	t.Cleanup(func() {
		tunnelManager.Stop()
	})
}
