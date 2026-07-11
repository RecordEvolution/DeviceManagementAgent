package apps

import (
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppEndpointURL(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		expected string
	}{
		{name: "localhost rewritten", in: "ws://localhost:8080/ws-re-dev", expected: "ws://host.docker.internal:8080/ws-re-dev"},
		{name: "loopback ip rewritten", in: "wss://127.0.0.1:8080/ws-re-dev", expected: "wss://host.docker.internal:8080/ws-re-dev"},
		{name: "ipv6 loopback rewritten", in: "ws://[::1]:8080/ws", expected: "ws://host.docker.internal:8080/ws"},
		{name: "no port", in: "ws://localhost/ws", expected: "ws://host.docker.internal/ws"},
		{name: "public host untouched", in: "wss://cbw.ironflock.com/ws-re-dev", expected: "wss://cbw.ironflock.com/ws-re-dev"},
		{name: "lan ip untouched", in: "wss://192.168.0.21:18080/ws-re-dev", expected: "wss://192.168.0.21:18080/ws-re-dev"},
		{name: "appliance host untouched", in: "wss://appliance.local:18080/ws-re-dev", expected: "wss://appliance.local:18080/ws-re-dev"},
		{name: "empty untouched", in: "", expected: ""},
		{name: "garbage untouched", in: "::not a url::", expected: "::not a url::"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, appEndpointURL(tc.in))
		})
	}
}

func TestAddComposeExtraHost(t *testing.T) {
	t.Run("adds when absent", func(t *testing.T) {
		service := map[string]interface{}{}
		addComposeExtraHost(service)
		assert.Equal(t, []interface{}{hostGatewayEntry}, service["extra_hosts"])
	})

	t.Run("appends to authored list", func(t *testing.T) {
		service := map[string]interface{}{"extra_hosts": []interface{}{"db.local:10.0.0.5"}}
		addComposeExtraHost(service)
		assert.Equal(t, []interface{}{"db.local:10.0.0.5", hostGatewayEntry}, service["extra_hosts"])
	})

	t.Run("respects authored mapping of the same name", func(t *testing.T) {
		service := map[string]interface{}{"extra_hosts": []interface{}{"host.docker.internal:172.17.0.1"}}
		addComposeExtraHost(service)
		assert.Equal(t, []interface{}{"host.docker.internal:172.17.0.1"}, service["extra_hosts"])
	})

	t.Run("map form", func(t *testing.T) {
		service := map[string]interface{}{"extra_hosts": map[string]interface{}{"db.local": "10.0.0.5"}}
		addComposeExtraHost(service)
		assert.Equal(t, map[string]interface{}{"db.local": "10.0.0.5", "host.docker.internal": "host-gateway"}, service["extra_hosts"])
	})
}

func TestTunnelDomainForApps(t *testing.T) {
	cases := []struct {
		name     string
		reswarm  config.ReswarmConfig
		expected string
	}{
		{name: "appliance domain wins", reswarm: config.ReswarmConfig{ApplianceDomain: "tunnel.factory.example", Environment: "production"}, expected: "tunnel.factory.example"},
		{name: "production cloud edge", reswarm: config.ReswarmConfig{Environment: "production"}, expected: "app.ironflock.com"},
		{name: "test cloud edge", reswarm: config.ReswarmConfig{Environment: "test"}, expected: "app.ironflock.dev"},
		{name: "local", reswarm: config.ReswarmConfig{Environment: "local"}, expected: "localhost"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reswarm := tc.reswarm
			cfg := &config.Config{ReswarmConfig: &reswarm}
			assert.Equal(t, tc.expected, tunnelDomainForApps(cfg))
		})
	}
}

// The env the agent hands every app: TUNNEL_DOMAIN always, INSTANCE_KEY only
// on instance devices — what SDK getRemoteAccessUrlForPort composes URLs from.
func TestBuildDefaultEnvironmentVariablesTunnelRouting(t *testing.T) {
	cfg := &config.Config{ReswarmConfig: &config.ReswarmConfig{Environment: "production", DeviceKey: 42}}
	app := &common.App{AppKey: 7, AppName: "myapp"}

	cloudEnv := buildDefaultEnvironmentVariables(cfg, common.TransitionPayload{}, common.PROD, app)
	assert.Contains(t, cloudEnv, "TUNNEL_DOMAIN=app.ironflock.com")
	for _, env := range cloudEnv {
		assert.NotContains(t, env, "INSTANCE_KEY=", "cloud devices must not get an instance key")
	}

	instanceEnv := buildDefaultEnvironmentVariables(cfg, common.TransitionPayload{InstanceKey: 5}, common.PROD, app)
	assert.Contains(t, instanceEnv, "INSTANCE_KEY=5")
}
