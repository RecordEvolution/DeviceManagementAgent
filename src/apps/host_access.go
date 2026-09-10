package apps

import (
	"net"
	"net/url"
	"reagent/common"
	"reagent/config"
	"reagent/trust"
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

// appCABundleHostDir returns the device-side directory holding the CA bundle
// app containers should verify TLS against, or "" when this device provides
// none. Callers mount only what exists, so a device where the bundle could not
// be built behaves exactly as it did before.
//
// See the trust package for why this is scoped to appliance-domain devices and
// why the bundle carries the whole host store rather than just the private CA.
func appCABundleHostDir(cfg *config.Config) string {
	if cfg == nil || cfg.ReswarmConfig == nil || cfg.CommandLineArguments == nil {
		return ""
	}
	if !trust.Enabled(cfg.ReswarmConfig.ApplianceDomain) {
		return ""
	}
	if !trust.Available(cfg.CommandLineArguments.AppsDirectory) {
		return ""
	}
	return trust.HostDir(cfg.CommandLineArguments.AppsDirectory)
}

// caBundleEnvironmentVariables points the common runtimes at the mounted
// bundle. SSL_CERT_FILE covers OpenSSL-based stacks (Python's ssl, .NET on
// Linux, Go), REQUESTS_CA_BUNDLE covers Python requests — which uses certifi
// and ignores the OpenSSL paths entirely — and NODE_EXTRA_CA_CERTS covers
// Node, where it ADDS to the built-in set rather than replacing it.
// IRONFLOCK_CA_BUNDLE is the name the SDKs can read when a runtime needs the
// path explicitly.
//
// These are defaults: buildProdEnvironmentVariables appends app-supplied
// variables after them, so an app that sets its own SSL_CERT_FILE still wins.
func caBundleEnvironmentVariables(cfg *config.Config) []string {
	if appCABundleHostDir(cfg) == "" {
		return nil
	}
	return []string{
		"SSL_CERT_FILE=" + trust.ContainerPath,
		"REQUESTS_CA_BUNDLE=" + trust.ContainerPath,
		"NODE_EXTRA_CA_CERTS=" + trust.ContainerPath,
		"IRONFLOCK_CA_BUNDLE=" + trust.ContainerPath,
	}
}

// addComposeCABundleMount mounts the CA bundle directory read-only into a
// compose service. Authored volumes are preserved; a service that already
// mounts something there keeps its own.
func addComposeCABundleMount(service map[string]interface{}, hostCADir string) {
	if hostCADir == "" {
		return
	}

	mountEntry := hostCADir + ":" + trust.ContainerDir + ":ro"

	switch volumes := service["volumes"].(type) {
	case nil:
		service["volumes"] = []interface{}{mountEntry}
	case []interface{}:
		for _, volume := range volumes {
			switch entry := volume.(type) {
			case string:
				if strings.Contains(entry, ":"+trust.ContainerDir) {
					return
				}
			case map[string]interface{}: // long syntax
				if target, ok := entry["target"].(string); ok && strings.HasPrefix(target, trust.ContainerDir) {
					return
				}
			}
		}
		service["volumes"] = append(volumes, mountEntry)
	default:
		// Unexpected shape — leave as authored.
	}
}
