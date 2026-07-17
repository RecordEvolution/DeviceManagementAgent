package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A real production frpc.yaml: a device running four apps over http, tcp and
// udp, with app names containing dashes and underscores. Only the http proxies
// carry a subdomain — tcp/udp store a remotePort instead — which is exactly the
// shape that used to make GetState drop every tcp/udp tunnel from the reported
// state and leave its port spinning in the UI forever.
const productionFrpcYAML = `
serverAddr: app.ironflock.com
serverPort: 7000
proxies:
    - name: 4749-wireguard_easy-51821-http
      type: http
      localPort: 51821
      subdomain: 4749-wireguard_easy-51821
    - name: 4749-node-red_runner-1881-http
      type: http
      localPort: 1881
      subdomain: 4749-node-red_runner-1881
    - name: 4749-wireguard_easy-51820-udp
      type: udp
      localPort: 40000
      remotePort: 32511
    - name: 4749-trumpfqds-56000-http
      type: http
      localPort: 40005
      subdomain: 4749-trumpfqds-56000
    - name: 4749-trumpfqds-1883-tcp
      type: tcp
      localPort: 40001
      remotePort: 31327
    - name: 4749-trumpfqds-445-tcp
      type: tcp
      localPort: 40012
      remotePort: 34216
    - name: 4749-trumpfqds-55000-http
      type: http
      localPort: 40003
      subdomain: 4749-trumpfqds-55000
`

// Every proxy in a real config must round-trip an identity equal to the name
// frpc reports status under -- GetState drops any that does not, and the port
// then spins in the UI forever. Covers the tcp/udp proxies that store no
// subdomain, and app names containing dashes and underscores.
func TestGetTunnelConfigMatchesEveryProductionProxy(t *testing.T) {
	var cfg FrpcYamlConfig
	require.NoError(t, yaml.Unmarshal([]byte(productionFrpcYAML), &cfg))

	builder := TunnelConfigBuilder{yamlConfig: &cfg, BaseTunnelURL: "app.ironflock.com"}
	frpTm := &FrpTunnelManager{configBuilder: builder}

	configs, err := builder.GetTunnelConfig()
	require.NoError(t, err)
	require.Len(t, configs, len(cfg.Proxies))

	wantDeclared := map[string]uint64{
		"4749-wireguard_easy-51821-http": 51821,
		"4749-node-red_runner-1881-http": 1881,
		"4749-wireguard_easy-51820-udp":  51820,
		"4749-trumpfqds-56000-http":      56000,
		"4749-trumpfqds-1883-tcp":        1883,
		"4749-trumpfqds-445-tcp":         445,
		"4749-trumpfqds-55000-http":      55000,
	}
	wantURL := map[string]string{
		"4749-wireguard_easy-51821-http": "https://4749-wireguard_easy-51821.app.ironflock.com",
		"4749-node-red_runner-1881-http": "https://4749-node-red_runner-1881.app.ironflock.com",
		"4749-wireguard_easy-51820-udp":  "udp://4749-wireguard_easy-51820.app.ironflock.com:32511",
		"4749-trumpfqds-56000-http":      "https://4749-trumpfqds-56000.app.ironflock.com",
		"4749-trumpfqds-1883-tcp":        "tcp://4749-trumpfqds-1883.app.ironflock.com:31327",
		"4749-trumpfqds-445-tcp":         "tcp://4749-trumpfqds-445.app.ironflock.com:34216",
		"4749-trumpfqds-55000-http":      "https://4749-trumpfqds-55000.app.ironflock.com",
	}

	for _, got := range configs {
		// The identity GetState matches against the frpc status name.
		assert.Equal(t, got.Name, CreateTunnelID(got.Subdomain, string(got.Protocol)),
			"%s: subdomain must rebuild the frpc proxy name", got.Name)
		// The declared port the UI keys its port rows on (never the host port).
		assert.Equal(t, wantDeclared[got.Name], got.DeclaredPort, "%s: declared port", got.Name)
		// A dropped subdomain also produced hosts with an empty first label.
		assert.Equal(t, wantURL[got.Name], frpTm.buildURL(got.Protocol, got.Subdomain, got.RemotePort),
			"%s: url", got.Name)
	}
}
