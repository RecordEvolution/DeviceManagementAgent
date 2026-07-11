package apps

import (
	"fmt"
	"strconv"
	"strings"
)

// wireProtocol maps a port rule protocol (http, https, tcp, udp) to the
// transport docker publishes on. Registry keys and docker binding lookups
// always use the wire protocol so rule-derived and compose-derived keys
// agree.
func wireProtocol(protocol string) string {
	if protocol == "udp" {
		return "udp"
	}
	return "tcp"
}

// composePortEntry is one parsed `ports:` list item of one compose service.
type composePortEntry struct {
	Service       string
	HostIP        string
	HostPort      uint64 // authored host port; 0 when only a container port is given
	ContainerPort uint64
	Protocol      string // wire protocol: "tcp" or "udp"
	Raw           interface{}
	// Rewritable is false for entries the agent must leave untouched
	// (ranges, compose variables, unparseable shapes).
	Rewritable bool
}

// DeclaredPort is the app-facing identity of the entry: the authored host
// port when present (that is what port rules and tunnels referenced before
// the agent managed host ports), the container port otherwise.
func (e composePortEntry) DeclaredPort() uint64 {
	if e.HostPort != 0 {
		return e.HostPort
	}
	return e.ContainerPort
}

// matchesRule reports whether a port rule refers to this entry.
func (e composePortEntry) matchesRule(rulePort uint64, ruleProtocol string) bool {
	return e.Rewritable && e.DeclaredPort() == rulePort && e.Protocol == wireProtocol(ruleProtocol)
}

// parseComposePorts extracts every `ports:` entry of every service from a
// compose definition (the JSON object shape the studio stores). Entries that
// cannot be parsed are returned with Rewritable=false so callers keep them
// byte-for-byte.
func parseComposePorts(dockerCompose map[string]interface{}) []composePortEntry {
	entries := make([]composePortEntry, 0)

	services, ok := dockerCompose["services"].(map[string]interface{})
	if !ok {
		return entries
	}

	for serviceName, serviceInterface := range services {
		service, ok := serviceInterface.(map[string]interface{})
		if !ok {
			continue
		}

		ports, ok := service["ports"].([]interface{})
		if !ok {
			continue
		}

		for _, rawEntry := range ports {
			entry := parseComposePortEntry(rawEntry)
			entry.Service = serviceName
			entries = append(entries, entry)
		}
	}

	return entries
}

func parseComposePortEntry(raw interface{}) composePortEntry {
	entry := composePortEntry{Raw: raw, Protocol: "tcp"}

	switch value := raw.(type) {
	case string:
		return parseShortSyntax(value, entry)
	case float64: // JSON numbers decode as float64: a bare container port
		entry.ContainerPort = uint64(value)
		entry.Rewritable = entry.ContainerPort > 0
		return entry
	case int:
		entry.ContainerPort = uint64(value)
		entry.Rewritable = entry.ContainerPort > 0
		return entry
	case map[string]interface{}: // long syntax
		return parseLongSyntax(value, entry)
	default:
		return entry
	}
}

// parseShortSyntax parses [[IP:]HOST:]CONTAINER[/PROTOCOL]. Ranges and
// compose variables are handed back untouched (Rewritable=false).
func parseShortSyntax(value string, entry composePortEntry) composePortEntry {
	spec := value

	if strings.Contains(spec, "$") { // "${WEB_PORT}:80" — resolved by compose, not us
		return entry
	}

	if slash := strings.LastIndex(spec, "/"); slash != -1 {
		protocol := strings.ToLower(spec[slash+1:])
		if protocol != "tcp" && protocol != "udp" {
			return entry
		}
		entry.Protocol = protocol
		spec = spec[:slash]
	}

	// Split from the right so IPv6 host addresses ("[::1]:8080:80") keep
	// their colons inside the IP part.
	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 1: // "80"
		// container port only
	case 2: // "8080:80"
		entry.HostPort = parsePort(parts[0])
		if entry.HostPort == 0 {
			return entry
		}
	default: // "127.0.0.1:8080:80", "[::1]:8080:80"
		entry.HostIP = strings.Join(parts[:len(parts)-2], ":")
		entry.HostPort = parsePort(parts[len(parts)-2])
		if entry.HostPort == 0 {
			return entry
		}
	}

	entry.ContainerPort = parsePort(parts[len(parts)-1])
	entry.Rewritable = entry.ContainerPort > 0
	return entry
}

func parseLongSyntax(value map[string]interface{}, entry composePortEntry) composePortEntry {
	entry.ContainerPort = parsePortValue(value["target"])
	if entry.ContainerPort == 0 {
		return entry
	}

	if protocol, ok := value["protocol"].(string); ok {
		protocol = strings.ToLower(protocol)
		if protocol != "tcp" && protocol != "udp" {
			return entry
		}
		entry.Protocol = protocol
	}

	if hostIP, ok := value["host_ip"].(string); ok {
		entry.HostIP = hostIP
	}

	if published, present := value["published"]; present {
		entry.HostPort = parsePortValue(published)
		if entry.HostPort == 0 { // range or variable in `published`
			return entry
		}
	}

	entry.Rewritable = true
	return entry
}

// parsePort parses a single port number; 0 for anything else (ranges,
// variables, garbage).
func parsePort(value string) uint64 {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0
	}
	return port
}

func parsePortValue(value interface{}) uint64 {
	switch v := value.(type) {
	case string:
		return parsePort(v)
	case float64:
		return uint64(v)
	case int:
		return uint64(v)
	default:
		return 0
	}
}

// rewriteComposePortEntry produces the replacement `ports:` value that
// publishes the entry's container port on bindIP:hostPort. The original Raw
// value is never mutated.
func rewriteComposePortEntry(entry composePortEntry, bindIP string, hostPort uint64) interface{} {
	if longSyntax, ok := entry.Raw.(map[string]interface{}); ok {
		rewritten := make(map[string]interface{}, len(longSyntax)+2)
		for key, value := range longSyntax {
			rewritten[key] = value
		}
		rewritten["published"] = strconv.FormatUint(hostPort, 10)
		rewritten["host_ip"] = bindIP
		return rewritten
	}

	spec := fmt.Sprintf("%s:%d:%d", bindIP, hostPort, entry.ContainerPort)
	if entry.Protocol == "udp" {
		spec += "/udp"
	}
	return spec
}
