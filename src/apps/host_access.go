package apps

import (
	"net"
	"net/url"
	"reagent/common"
	"reagent/config"
	"reagent/tunnel"
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

// tunnelDomainForApps is the domain direct device tunnels are published
// under: the appliance/operator tunnel domain when configured, the
// environment's cloud tunnel edge otherwise. Mirrors the server-address
// resolution in tunnel.initialize().
func tunnelDomainForApps(cfg *config.Config) string {
	if cfg.ReswarmConfig.ApplianceDomain != "" {
		return cfg.ReswarmConfig.ApplianceDomain
	}

	switch cfg.ReswarmConfig.Environment {
	case string(common.TEST):
		return tunnel.TEST_SERVER_ADDR
	case string(common.LOCAL):
		return "localhost"
	default:
		return tunnel.PROD_SERVER_ADDR
	}
}

// addComposeEnvFilesMount mounts the agent's env-files directory read-only at
// /data/env in a compose service, mirroring the single-container /data bind:
// it is how live env updates (e.g. a late-allocated tunnel cloud port) reach
// running compose containers. Authored volumes are preserved; a service that
// already mounts something at /data/env keeps its own mount.
func addComposeEnvFilesMount(service map[string]interface{}, hostEnvDir string) {
	mountEntry := hostEnvDir + ":/data/env:ro"

	switch volumes := service["volumes"].(type) {
	case nil:
		service["volumes"] = []interface{}{mountEntry}
	case []interface{}:
		for _, volume := range volumes {
			switch entry := volume.(type) {
			case string:
				if strings.Contains(entry, ":/data/env") {
					return
				}
			case map[string]interface{}: // long syntax
				if target, ok := entry["target"].(string); ok && strings.HasPrefix(target, "/data/env") {
					return
				}
			}
		}
		service["volumes"] = append(volumes, mountEntry)
	default:
		// Unexpected shape — leave as authored.
	}
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
