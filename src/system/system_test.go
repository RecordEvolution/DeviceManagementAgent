package system

import (
	"testing"

	"reagent/common"
	"reagent/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnv(t *testing.T) {
	const key = "REAGENT_TEST_ENV_VAR_XYZ"

	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv(key, "hello")
		assert.Equal(t, "hello", GetEnv(key))
		assert.Equal(t, "hello", GetEnv(key, "fallback"))
	})

	t.Run("returns default when unset", func(t *testing.T) {
		// Ensure unset for this subtest.
		t.Setenv(key, "")
		assert.Equal(t, "fallback", GetEnv(key, "fallback"))
	})

	t.Run("returns empty string when unset and no default", func(t *testing.T) {
		t.Setenv(key, "")
		assert.Equal(t, "", GetEnv(key))
	})

	t.Run("uses first default only", func(t *testing.T) {
		t.Setenv(key, "")
		assert.Equal(t, "first", GetEnv(key, "first", "second"))
	})

	t.Run("set value wins over default", func(t *testing.T) {
		t.Setenv(key, "actual")
		assert.Equal(t, "actual", GetEnv(key, "fallback"))
	})
}

func TestGetEnvironment(t *testing.T) {
	newCfg := func(env, endpoint string) *config.Config {
		return &config.Config{
			ReswarmConfig: &config.ReswarmConfig{
				Environment:       env,
				DeviceEndpointURL: endpoint,
			},
		}
	}

	tests := []struct {
		name     string
		env      string
		endpoint string
		want     string
	}{
		{
			name:     "explicit environment wins over endpoint",
			env:      "production",
			endpoint: "wss://cbw.datapods.example/ws",
			want:     "production",
		},
		{
			name:     "explicit environment is returned verbatim",
			env:      "staging",
			endpoint: "",
			want:     "staging",
		},
		{
			name:     "datapods endpoint maps to test",
			env:      "",
			endpoint: "wss://cbw.datapods.io/ws-re-dev",
			want:     "test",
		},
		{
			name:     "record-evolution endpoint maps to production",
			env:      "",
			endpoint: "wss://cbw.record-evolution.com/ws-re-dev",
			want:     "production",
		},
		{
			name:     "unknown endpoint falls back to local",
			env:      "",
			endpoint: "ws://localhost:8080/ws",
			want:     "local",
		},
		{
			name:     "empty env and empty endpoint falls back to local",
			env:      "",
			endpoint: "",
			want:     "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetEnvironment(newCfg(tt.env, tt.endpoint))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCompareVersion(t *testing.T) {
	// compareVersion does not read any System fields; a zero value is enough.
	sys := &System{}

	tests := []struct {
		name       string
		current    string
		latest     string
		wantUpdate bool
		wantErr    bool
	}{
		{
			name:       "newer patch should update",
			current:    "1.2.3",
			latest:     "1.2.4",
			wantUpdate: true,
		},
		{
			name:       "newer minor should update",
			current:    "1.2.3",
			latest:     "1.3.0",
			wantUpdate: true,
		},
		{
			name:       "equal version should not update",
			current:    "1.2.3",
			latest:     "1.2.3",
			wantUpdate: false,
		},
		{
			name:       "older version should not update",
			current:    "1.2.3",
			latest:     "1.2.2",
			wantUpdate: false,
		},
		{
			name:    "invalid current version errors",
			current: "not-a-version",
			latest:  "1.0.0",
			wantErr: true,
		},
		{
			name:    "invalid latest version errors",
			current: "1.0.0",
			latest:  "not-a-version",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldUpdate, _, err := sys.compareVersion(tt.current, tt.latest)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUpdate, shouldUpdate)
		})
	}
}

func TestUpdateDeviceConfig(t *testing.T) {
	base := func() *DeviceConfig {
		return &DeviceConfig{
			BootConfig:        "boot",
			Cmdline:           "cmd",
			NetworkInterfaces: "net",
		}
	}

	t.Run("identical config reports no update", func(t *testing.T) {
		updated, err := UpdateDeviceConfig(base(), base())
		require.NoError(t, err)
		assert.False(t, updated)
	})

	t.Run("changed boot config reports update", func(t *testing.T) {
		newConf := base()
		newConf.BootConfig = "boot-changed"
		updated, err := UpdateDeviceConfig(base(), newConf)
		require.NoError(t, err)
		assert.True(t, updated)
	})

	t.Run("changed cmdline reports update", func(t *testing.T) {
		newConf := base()
		newConf.Cmdline = "cmd-changed"
		updated, err := UpdateDeviceConfig(base(), newConf)
		require.NoError(t, err)
		assert.True(t, updated)
	})

	t.Run("changed network interfaces reports update", func(t *testing.T) {
		newConf := base()
		newConf.NetworkInterfaces = "net-changed"
		updated, err := UpdateDeviceConfig(base(), newConf)
		require.NoError(t, err)
		assert.True(t, updated)
	})
}

func TestBuildDeviceConfigFromPayload(t *testing.T) {
	validPayload := func() *common.Result {
		return &common.Result{
			Arguments: []interface{}{
				[]interface{}{
					map[string]interface{}{
						"boot_config":        "the-boot-config",
						"cmdline":            "the-cmdline",
						"network_interfaces": "the-network-interfaces",
					},
				},
			},
		}
	}

	t.Run("parses a well-formed payload", func(t *testing.T) {
		conf, err := buildDeviceConfigFromPayload(validPayload())
		require.NoError(t, err)
		assert.Equal(t, "the-boot-config", conf.BootConfig)
		assert.Equal(t, "the-cmdline", conf.Cmdline)
		assert.Equal(t, "the-network-interfaces", conf.NetworkInterfaces)
	})

	t.Run("errors when outer argument is not an array", func(t *testing.T) {
		res := &common.Result{Arguments: []interface{}{"not-an-array"}}
		_, err := buildDeviceConfigFromPayload(res)
		require.Error(t, err)
	})

	t.Run("errors when inner element is not a map", func(t *testing.T) {
		res := &common.Result{Arguments: []interface{}{[]interface{}{"not-a-map"}}}
		_, err := buildDeviceConfigFromPayload(res)
		require.Error(t, err)
	})

	t.Run("errors when boot_config missing", func(t *testing.T) {
		res := validPayload()
		inner := res.Arguments[0].([]interface{})[0].(map[string]interface{})
		delete(inner, "boot_config")
		_, err := buildDeviceConfigFromPayload(res)
		require.Error(t, err)
	})

	t.Run("errors when cmdline has wrong type", func(t *testing.T) {
		res := validPayload()
		inner := res.Arguments[0].([]interface{})[0].(map[string]interface{})
		inner["cmdline"] = 123
		_, err := buildDeviceConfigFromPayload(res)
		require.Error(t, err)
	})

	t.Run("errors when network_interfaces missing", func(t *testing.T) {
		res := validPayload()
		inner := res.Arguments[0].([]interface{})[0].(map[string]interface{})
		delete(inner, "network_interfaces")
		_, err := buildDeviceConfigFromPayload(res)
		require.Error(t, err)
	})
}
