package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/config"
	"reagent/container"
	"reagent/errdefs"
	"reagent/tunnel"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/rs/zerolog/log"
)

// hostPortKeyForRule builds the (non-compose, unqualified) registry key for a
// tunnel port rule.
func hostPortKeyForRule(stage common.Stage, appKey uint64, rule common.PortForwardRule) hostPortKey {
	return hostPortKey{Stage: stage, AppKey: appKey, Protocol: wireProtocol(rule.Protocol), Port: rule.Port}
}

// composeFilePathFor returns where SetupComposeFiles writes the app's compose
// file (mirrors its directory layout).
func composeFilePathFor(cfg *config.Config, stage common.Stage, appName string) string {
	targetDir := cfg.CommandLineArguments.AppsBuildDir
	if stage == common.PROD {
		targetDir = cfg.CommandLineArguments.AppsComposeDir
	}
	return targetDir + "/" + appName + "/" + DockerFileName
}

// bindingKey is how GetContainerPortBindings keys a container port.
func bindingKey(port uint64, protocol string) string {
	return fmt.Sprintf("%d/%s", port, protocol)
}

// resolveTunnelHostPort returns the local port frpc should dial for a port
// rule. It never allocates fresh pool ports — that happens at container
// launch — it only reads the registry or recovers assignments from the app's
// live docker state and the host_port persisted upstream.
//
// ok=false means the app has not been started with managed ports yet; the
// caller skips tunnel creation and the post-transition sync retries.
func (am *AppManager) resolveTunnelHostPort(payload common.TransitionPayload, rule common.PortForwardRule) (uint64, bool) {
	stage, appKey := payload.Stage, payload.AppKey
	protocol := wireProtocol(rule.Protocol)

	if hostPort, ok := am.hostPorts.GetByPort(stage, appKey, protocol, rule.Port); ok {
		return hostPort, true
	}

	key := hostPortKeyForRule(stage, appKey, rule)
	preferred := rule.HostPort

	if payload.DockerCompose != nil {
		service, published := am.findComposeRulePort(payload, rule)
		if service != "" {
			// Use the service-qualified key so the recovery converges with
			// the assignment SetupComposeFiles makes for the same entry.
			key.Service = service
		}
		if published != 0 {
			preferred = published
		}
	} else {
		containerName := payload.ContainerName.Prod
		if stage == common.DEV {
			containerName = payload.ContainerName.Dev
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		bindings, err := am.StateMachine.Container.GetContainerPortBindings(ctx, containerName)
		if err == nil {
			if bound := bindings[bindingKey(rule.Port, protocol)]; bound != 0 {
				preferred = bound
			} else if len(bindings) == 0 {
				// A pre-migration container still running with host
				// networking: the declared port IS the host port. Dial it
				// directly without recording a managed assignment — the
				// container is recreated onto a bridge on its next start.
				networkMode, err := am.StateMachine.Container.GetContainerNetworkMode(ctx, containerName)
				if err == nil && networkMode == "host" {
					return rule.Port, true
				}
			}
		}
	}

	if preferred == 0 {
		return 0, false
	}

	hostPort, err := am.hostPorts.RecoverOrReserve(key, preferred)
	if err != nil {
		log.Error().Err(err).Str("app", payload.AppName).Uint64("port", rule.Port).Msg("Failed to recover host port for tunnel")
		return 0, false
	}
	return hostPort, true
}

// findComposeRulePort locates the compose entry a port rule refers to and
// returns its service name plus the host port the running project currently
// publishes it on (0 when the project is not up).
func (am *AppManager) findComposeRulePort(payload common.TransitionPayload, rule common.PortForwardRule) (string, uint64) {
	for _, entry := range parseComposePorts(payload.DockerCompose) {
		if !entry.matchesRule(rule.Port, rule.Protocol) {
			continue
		}

		cfg := am.StateMachine.Container.GetConfig()
		composePath := composeFilePathFor(cfg, payload.Stage, payload.AppName)
		published, err := am.StateMachine.Container.Compose().GetPublishedPorts(composePath)
		if err != nil {
			log.Debug().Err(err).Str("app", payload.AppName).Msg("Could not read published compose ports")
			return entry.Service, 0
		}

		return entry.Service, published[container.PublishedPortKey(entry.Service, entry.ContainerPort, entry.Protocol)]
	}
	return "", 0
}

// reserveLaunchHostPort picks the host port to publish a single-container
// app's port rule on at (re)creation time, allocating from the pool when
// nothing can be recovered. DEV apps get their declared port when no other
// app holds it, so developers keep reaching dev apps on the port they wrote;
// the caller surfaces the fallback port in the app log when that loses.
func (am *AppManager) reserveLaunchHostPort(payload common.TransitionPayload, rule common.PortForwardRule, liveBindings map[string]uint64) (uint64, error) {
	key := hostPortKeyForRule(payload.Stage, payload.AppKey, rule)

	preferred := liveBindings[bindingKey(rule.Port, key.Protocol)]
	if preferred == 0 {
		preferred = rule.HostPort
	}

	if payload.Stage == common.DEV && preferred == 0 && am.hostPorts.ReserveDeclared(key) {
		return key.Port, nil
	}

	return am.hostPorts.RecoverOrReserve(key, preferred)
}

// computePortBindings builds the ExposedPorts/PortBindings that publish each
// of the app's declared ports on its agent-managed host port, bound on all
// interfaces so devices stay reachable on the LAN.
func (sm *StateMachine) computePortBindings(payload common.TransitionPayload, portRules []common.PortForwardRule, containerName string) (nat.PortSet, nat.PortMap, error) {
	am := sm.StateObserver.AppManager

	publishableRules := make([]common.PortForwardRule, 0, len(portRules))
	for _, rule := range portRules {
		// Rules with an explicit LocalIP point away from this container;
		// nothing to publish.
		if rule.LocalIP == "" {
			publishableRules = append(publishableRules, rule)
		}
	}

	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	if len(publishableRules) == 0 {
		return exposedPorts, portBindings, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	liveBindings, err := sm.Container.GetContainerPortBindings(ctx, containerName)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			log.Debug().Err(err).Str("container", containerName).Msg("Could not inspect existing container for host port recovery")
		}
		liveBindings = map[string]uint64{}
	}

	for _, rule := range publishableRules {
		hostPort, err := am.reserveLaunchHostPort(payload, rule, liveBindings)
		if err != nil {
			return nil, nil, err
		}

		if payload.Stage == common.DEV && hostPort != rule.Port {
			sm.LogManager.Write(containerName, fmt.Sprintf("Port %d is already in use by another app; port %d of %s is reachable on host port %d instead.", rule.Port, rule.Port, payload.AppName, hostPort))
		}

		containerPort, err := nat.NewPort(wireProtocol(rule.Protocol), strconv.FormatUint(rule.Port, 10))
		if err != nil {
			return nil, nil, err
		}

		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.FormatUint(hostPort, 10)}}
	}

	return exposedPorts, portBindings, nil
}

// devicePortEnvLines renders one DEVICE_PORT_FOR_<declared port>=<host port>
// line per entry, sorted by declared port.
func devicePortEnvLines(hostPortByDeclared map[uint64]uint64) []string {
	declaredPorts := make([]uint64, 0, len(hostPortByDeclared))
	for declared := range hostPortByDeclared {
		declaredPorts = append(declaredPorts, declared)
	}
	sort.Slice(declaredPorts, func(i, j int) bool { return declaredPorts[i] < declaredPorts[j] })

	lines := make([]string, 0, len(declaredPorts))
	for _, declared := range declaredPorts {
		lines = append(lines, fmt.Sprintf("DEVICE_PORT_FOR_%d=%d", declared, hostPortByDeclared[declared]))
	}
	return lines
}

// devicePortEnvsFromBindings builds the DEVICE_PORT_FOR_<port> environment
// variables announcing which host port each published port landed on.
// Single-container apps publish declared ports verbatim, so the container
// port doubles as the declared port. When tcp and udp share a declared port
// the tcp binding wins (deterministic via the sorted "port/proto" keys).
func devicePortEnvsFromBindings(portBindings nat.PortMap) []string {
	containerPorts := make([]nat.Port, 0, len(portBindings))
	for containerPort := range portBindings {
		containerPorts = append(containerPorts, containerPort)
	}
	sort.Slice(containerPorts, func(i, j int) bool { return containerPorts[i] < containerPorts[j] })

	hostPortByDeclared := make(map[uint64]uint64, len(containerPorts))
	for _, containerPort := range containerPorts {
		declared := uint64(containerPort.Int())
		if _, ok := hostPortByDeclared[declared]; ok {
			continue
		}
		for _, hostBinding := range portBindings[containerPort] {
			hostPort, err := strconv.ParseUint(hostBinding.HostPort, 10, 64)
			if err == nil && hostPort != 0 {
				hostPortByDeclared[declared] = hostPort
				break
			}
		}
	}

	return devicePortEnvLines(hostPortByDeclared)
}

// devicePortEnvsForCompose builds the DEVICE_PORT_FOR_<port> environment
// variables for a compose app by reading the registry assignments
// rewriteComposeHostPorts recorded for it. dockerCompose must be the authored
// definition — the rewrite replaces the authored host ports, which are the
// entries' declared identity. Unmanaged entries (ranges, variables) and
// container-only ports no rule publishes carry no assignment and are skipped.
// When two services share a declared port the first (sorted by service) wins.
func (am *AppManager) devicePortEnvsForCompose(payload common.TransitionPayload, dockerCompose map[string]interface{}) []string {
	entries := parseComposePorts(dockerCompose)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].DeclaredPort() != entries[j].DeclaredPort() {
			return entries[i].DeclaredPort() < entries[j].DeclaredPort()
		}
		if entries[i].Service != entries[j].Service {
			return entries[i].Service < entries[j].Service
		}
		return entries[i].Protocol < entries[j].Protocol
	})

	hostPortByDeclared := make(map[uint64]uint64)
	for _, entry := range entries {
		if !entry.Rewritable {
			continue
		}
		if _, ok := hostPortByDeclared[entry.DeclaredPort()]; ok {
			continue
		}

		key := hostPortKey{Stage: payload.Stage, AppKey: payload.AppKey, Protocol: entry.Protocol, Port: entry.DeclaredPort(), Service: entry.Service}
		if hostPort, ok := am.hostPorts.Get(key); ok {
			hostPortByDeclared[entry.DeclaredPort()] = hostPort
		}
	}

	return devicePortEnvLines(hostPortByDeclared)
}

// rewriteComposeHostPorts replaces the host side of every published compose
// port with an agent-managed host port bound on all interfaces, so compose
// apps cannot collide on host ports. Container-only entries are published
// only when a port rule references them; entries the parser cannot handle
// (ranges, compose variables) are left byte-for-byte as authored.
//
// The map passed in must be a copy of the authored compose definition — the
// rewrite mutates it.
func (sm *StateMachine) rewriteComposeHostPorts(payload common.TransitionPayload, dockerCompose map[string]interface{}) error {
	am := sm.StateObserver.AppManager
	cfg := sm.Container.GetConfig()

	// Host ports of a still-running previous project generation: recovering
	// them keeps ports stable across restarts and agent updates.
	composePath := composeFilePathFor(cfg, payload.Stage, payload.AppName)
	published, err := sm.Container.Compose().GetPublishedPorts(composePath)
	if err != nil {
		log.Debug().Err(err).Str("app", payload.AppName).Msg("Could not read published compose ports")
		published = map[string]uint64{}
	}

	portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
	if err != nil {
		return err
	}

	services, ok := dockerCompose["services"].(map[string]interface{})
	if !ok {
		return nil
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

		rewritten := make([]interface{}, 0, len(ports))
		for _, raw := range ports {
			entry := parseComposePortEntry(raw)
			entry.Service = serviceName

			if !entry.Rewritable {
				log.Warn().Str("app", payload.AppName).Str("service", serviceName).Interface("entry", raw).Msg("Compose port entry left unmanaged (range, variable or unparseable); it may collide with other apps")
				rewritten = append(rewritten, raw)
				continue
			}

			var matchingRule *common.PortForwardRule
			for i, rule := range portRules {
				if rule.LocalIP == "" && entry.matchesRule(rule.Port, rule.Protocol) {
					matchingRule = &portRules[i]
					break
				}
			}

			if entry.HostPort == 0 && matchingRule == nil {
				// Container-only port no rule asks for: keep it unpublished.
				rewritten = append(rewritten, raw)
				continue
			}

			preferred := published[container.PublishedPortKey(serviceName, entry.ContainerPort, entry.Protocol)]
			if preferred == 0 && matchingRule != nil {
				preferred = matchingRule.HostPort
			}

			key := hostPortKey{Stage: payload.Stage, AppKey: payload.AppKey, Protocol: entry.Protocol, Port: entry.DeclaredPort(), Service: serviceName}

			var hostPort uint64
			if payload.Stage == common.DEV && preferred == 0 && am.hostPorts.ReserveDeclared(key) {
				// DEV keeps the authored host port so developers reach the
				// app where they expect it.
				hostPort = key.Port
			} else {
				hostPort, err = am.hostPorts.RecoverOrReserve(key, preferred)
				if err != nil {
					return err
				}

				if payload.Stage == common.DEV && hostPort != entry.DeclaredPort() {
					sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Port %d is already in use by another app; port %d of service %s is reachable on host port %d instead.", entry.DeclaredPort(), entry.ContainerPort, serviceName, hostPort))
				}
			}

			rewritten = append(rewritten, rewriteComposePortEntry(entry, "0.0.0.0", hostPort))
		}

		service["ports"] = rewritten
	}

	return nil
}

// containerNetworkConfigOutdated reports whether an existing container must
// be recreated because its network mode or published ports no longer match
// the desired host config.
func (sm *StateMachine) containerNetworkConfigOutdated(cont types.Container, hConfig *dockercontainer.HostConfig, containerName string) bool {
	if normalizeNetworkMode(cont.HostConfig.NetworkMode) != normalizeNetworkMode(string(hConfig.NetworkMode)) {
		return true
	}

	if len(hConfig.PortBindings) == 0 {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	actualBindings, err := sm.Container.GetContainerPortBindings(ctx, containerName)
	if err != nil {
		return false
	}

	desiredBindings := make(map[string]uint64, len(hConfig.PortBindings))
	for containerPort, hostBindings := range hConfig.PortBindings {
		for _, hostBinding := range hostBindings {
			hostPort, err := strconv.ParseUint(hostBinding.HostPort, 10, 64)
			if err == nil {
				desiredBindings[string(containerPort)] = hostPort
			}
		}
	}

	if len(desiredBindings) != len(actualBindings) {
		return true
	}
	for containerPort, hostPort := range desiredBindings {
		if actualBindings[containerPort] != hostPort {
			return true
		}
	}

	return false
}

func normalizeNetworkMode(mode string) string {
	if mode == "" || mode == "default" {
		return "bridge"
	}
	return mode
}

// isPortAllocationError reports whether a container create/start failure is a
// host port conflict (something outside the registry grabbed the port between
// probe and bind).
func isPortAllocationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "port is already allocated") || strings.Contains(message, "address already in use")
}

// reassignPortsAfterBindConflict drops the app's registry assignments that
// appear in a bind-conflict error and allocates fresh ports for them. Returns
// true when at least one port was reassigned, i.e. a retry makes sense.
func (am *AppManager) reassignPortsAfterBindConflict(payload common.TransitionPayload, bindErr error) bool {
	portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
	if err != nil {
		return false
	}

	message := bindErr.Error()
	reassigned := false
	for _, rule := range portRules {
		if rule.LocalIP != "" {
			continue
		}

		key := hostPortKeyForRule(payload.Stage, payload.AppKey, rule)
		hostPort, ok := am.hostPorts.Get(key)
		if !ok || !strings.Contains(message, fmt.Sprintf(":%d", hostPort)) {
			continue
		}

		newPort, err := am.hostPorts.ReassignFresh(key)
		if err != nil {
			log.Error().Err(err).Uint64("port", rule.Port).Msg("Failed to reassign host port after bind conflict")
			continue
		}

		log.Warn().Str("app", payload.AppName).Uint64("port", rule.Port).Uint64("oldHostPort", hostPort).Uint64("newHostPort", newPort).Msg("Host port was taken by another process, reassigned")
		reassigned = true
	}

	return reassigned
}
