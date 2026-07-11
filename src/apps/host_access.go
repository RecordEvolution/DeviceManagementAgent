package apps

import (
	"net"
	"net/url"
	"strings"
)

// hostGatewayEntry makes host.docker.internal resolve inside app containers
// on plain Linux docker; Docker Desktop (macOS/Windows) provides the name
// natively and ignores nothing — the explicit mapping is simply redundant
// there. Supported since docker 20.10.
const hostGatewayEntry = "host.docker.internal:host-gateway"

// appEndpointURL returns the device endpoint URL as seen from inside an app
// container. The agent dials the configured URL from the host network
// namespace, where a loopback host is the device itself; a bridge-networked
// app container's loopback is the container. Loopback endpoints (local dev
// setups — production devices point at a public or LAN address) are therefore
// rewritten to host.docker.internal, which the agent maps to the host gateway
// on every container it creates.
func appEndpointURL(endpointURL string) string {
	parsed, err := url.Parse(endpointURL)
	if err != nil {
		return endpointURL
	}

	host := parsed.Hostname()
	if host == "" {
		return endpointURL
	}

	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return endpointURL
		}
	}

	if port := parsed.Port(); port != "" {
		parsed.Host = "host.docker.internal:" + port
	} else {
		parsed.Host = "host.docker.internal"
	}

	return parsed.String()
}

// addComposeExtraHost injects the host-gateway mapping into a compose
// service, preserving whatever extra_hosts the author already declared.
func addComposeExtraHost(service map[string]interface{}) {
	switch hosts := service["extra_hosts"].(type) {
	case nil:
		service["extra_hosts"] = []interface{}{hostGatewayEntry}
	case []interface{}:
		for _, entry := range hosts {
			if s, ok := entry.(string); ok && strings.HasPrefix(s, "host.docker.internal") {
				return
			}
		}
		service["extra_hosts"] = append(hosts, hostGatewayEntry)
	case map[string]interface{}:
		if _, present := hosts["host.docker.internal"]; !present {
			hosts["host.docker.internal"] = "host-gateway"
		}
	}
}
