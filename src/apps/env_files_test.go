package apps

import (
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envFilesTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		CommandLineArguments: &config.CommandLineArguments{AppsDirectory: t.TempDir()},
		ReswarmConfig:        &config.ReswarmConfig{},
	}
}

func readEnvFile(t *testing.T, cfg *config.Config, name string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appEnvFilesHostDir(cfg, common.PROD, "MyApp"), name+".txt"))
	if err != nil {
		require.True(t, os.IsNotExist(err))
		return "", false
	}
	return string(data), true
}

// The live-refresh channel: {NAME}.txt and {NAME}_CLOUD.txt track the current
// tunnel ports; a cloud port that disappeared removes its file so SDKs fall
// back to the LAN URL.
func TestRefreshRemotePortEnvFiles(t *testing.T) {
	cfg := envFilesTestConfig(t)

	rules := []common.PortForwardRule{
		{RuleName: "vpn", Port: 51820, Protocol: "udp", RemotePortEnvironment: "WG_PORT", RemotePort: 30022, CloudRemotePort: 31099},
		{RuleName: "web", Port: 8080, Protocol: "http"}, // no remote_port_environment — ignored
	}

	refreshRemotePortEnvFiles(cfg, common.PROD, "MyApp", rules)

	value, ok := readEnvFile(t, cfg, "WG_PORT")
	assert.True(t, ok)
	assert.Equal(t, "30022", value)

	value, ok = readEnvFile(t, cfg, "WG_PORT_CLOUD")
	assert.True(t, ok)
	assert.Equal(t, "31099", value)

	// Cloud port changes → file content follows.
	rules[0].CloudRemotePort = 31100
	refreshRemotePortEnvFiles(cfg, common.PROD, "MyApp", rules)
	value, _ = readEnvFile(t, cfg, "WG_PORT_CLOUD")
	assert.Equal(t, "31100", value)

	// Forwarding off → cloud file removed, base port file stays.
	rules[0].CloudRemotePort = 0
	refreshRemotePortEnvFiles(cfg, common.PROD, "MyApp", rules)
	_, ok = readEnvFile(t, cfg, "WG_PORT_CLOUD")
	assert.False(t, ok)
	_, ok = readEnvFile(t, cfg, "WG_PORT")
	assert.True(t, ok)
}

// Apps without remote-port env rules must not even get an env dir created.
func TestRefreshRemotePortEnvFilesNoRelevantRules(t *testing.T) {
	cfg := envFilesTestConfig(t)

	refreshRemotePortEnvFiles(cfg, common.PROD, "MyApp", []common.PortForwardRule{
		{RuleName: "web", Port: 8080, Protocol: "http"},
	})

	_, err := os.Stat(appEnvFilesHostDir(cfg, common.PROD, "MyApp"))
	assert.True(t, os.IsNotExist(err))
}

func TestAddComposeEnvFilesMount(t *testing.T) {
	const hostDir = "/apps/prod/myapp/env"
	expected := hostDir + ":/data/env:ro"

	t.Run("adds when absent", func(t *testing.T) {
		service := map[string]interface{}{}
		addComposeEnvFilesMount(service, hostDir)
		assert.Equal(t, []interface{}{expected}, service["volumes"])
	})

	t.Run("appends to authored volumes", func(t *testing.T) {
		service := map[string]interface{}{"volumes": []interface{}{"mydata:/data"}}
		addComposeEnvFilesMount(service, hostDir)
		assert.Equal(t, []interface{}{"mydata:/data", expected}, service["volumes"])
	})

	t.Run("respects authored /data/env mount", func(t *testing.T) {
		service := map[string]interface{}{"volumes": []interface{}{"own:/data/env"}}
		addComposeEnvFilesMount(service, hostDir)
		assert.Equal(t, []interface{}{"own:/data/env"}, service["volumes"])
	})

	t.Run("respects long-syntax /data/env mount", func(t *testing.T) {
		authored := map[string]interface{}{"type": "bind", "source": "/x", "target": "/data/env"}
		service := map[string]interface{}{"volumes": []interface{}{authored}}
		addComposeEnvFilesMount(service, hostDir)
		assert.Equal(t, []interface{}{authored}, service["volumes"])
	})
}
