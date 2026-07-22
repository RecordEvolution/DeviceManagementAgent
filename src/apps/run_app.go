package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	reagentnetwork "reagent/network"
	"reagent/system"
	"reagent/tunnel"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) runApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.runDevApp(payload, app)
	} else if payload.Stage == common.PROD {
		return sm.runProdApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) runProdApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.runProdComposeApp(payload, app)
	}

	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Starting %s...", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	containerID, err := sm.startContainer(payload, app)
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	waitForRunningContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	runningSignal, errC := sm.Container.WaitForRunning(waitForRunningContext, containerID, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("The app failed to start, reason: %s", err.Error()))
		options := common.Dict{"follow": true, "stdout": true, "stderr": true}
		ioReader, logsErr := sm.Container.Logs(context.Background(), containerID, options)
		if logsErr != nil {
			return logsErr
		}

		sm.LogManager.StreamBlocking(payload.ContainerName.Prod, common.APP, ioReader)
		return err
	case <-runningSignal:
		break
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Now running app %s (%s)", payload.AppName, app.Version))
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Prod, common.APP, nil)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) runDevComposeApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	// Mark STARTING before SetupComposeFiles: that step reads the previous
	// generation's published ports from the Docker API, which can be slow on a
	// contended daemon; leaving the app in PRESENT until then misleads the UI.
	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, false)
	if err != nil {
		return err
	}

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	_, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, cmd, err = compose.Remove(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Starting %s...", payload.AppName))
	if err != nil {
		return err
	}

	outputChan, upCmd, err := compose.Up(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(outputChan, payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = upCmd.Wait()
	if err != nil {
		return err
	}

	waitForRunningContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	pollingRate := time.Second * 1
	runningSignal, errC := compose.WaitForRunning(waitForRunningContext, dockerComposePath, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		if err != nil {
			// cleanup docker containers
			_, cmd, cleanupErr := compose.Stop(dockerComposePath)
			if cleanupErr != nil {
				return cleanupErr
			}

			cleanupErr = cmd.Wait()
			if cleanupErr != nil {
				return cleanupErr
			}

			_, cmd, cleanupErr = compose.Remove(dockerComposePath)
			if cleanupErr != nil {
				return cleanupErr
			}

			cleanupErr = cmd.Wait()
			if cleanupErr != nil {
				return cleanupErr
			}

			sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("The app failed to start, reason: %s", err.Error()))
			return err
		}
		break
	case <-runningSignal:
		break
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Now running app %s (%s)", payload.AppName, app.Version))
	if err != nil {
		return err
	}

	logsChannel, err := compose.LogStream(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(logsChannel, payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) runProdComposeApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()
	if !compose.Supported {
		message := "Docker Compose is not supported for this device"
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, message)
		if writeErr != nil {
			return writeErr
		}

		return errdefs.DockerComposeNotSupported(errors.New("docker compose is not supported"))
	}

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	// Mark STARTING before SetupComposeFiles: on a contended boot that step
	// (a Docker-API read of the previous generation's published ports) can take
	// several seconds, and until the app leaves PRESENT the cloud sees it as
	// not-yet-starting while re-drive pushes just bounce off the held lock.
	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, false)
	if err != nil {
		return err
	}

	_, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, cmd, err = compose.Remove(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Starting %s...", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	outputChan, upCmd, err := compose.Up(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(outputChan, payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	err = upCmd.Wait()
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	waitForRunningContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	runningSignal, errC := compose.WaitForRunning(waitForRunningContext, dockerComposePath, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		if err != nil {
			// cleanup docker containers
			_, cmd, cleanupErr := compose.Stop(dockerComposePath)
			if cleanupErr != nil {
				return cleanupErr
			}

			cleanupErr = cmd.Wait()
			if cleanupErr != nil {
				return cleanupErr
			}

			_, cmd, cleanupErr = compose.Remove(dockerComposePath)
			if cleanupErr != nil {
				return cleanupErr
			}

			cleanupErr = cmd.Wait()
			if cleanupErr != nil {
				return cleanupErr
			}

			sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("The app failed to start, reason: %s", err.Error()))
			return err
		}
		break
	case <-runningSignal:
		break
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Now running app %s (%s)", payload.AppName, app.Version))
	if err != nil {
		return err
	}

	logsChannel, err := compose.LogStream(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(logsChannel, payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) runDevApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.runDevComposeApp(payload, app)
	}

	getContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// remove old container first, if it exists
	cont, err := sm.Container.GetContainer(getContainerContext, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		RemoveContainerByIDContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		removeContainerErr := sm.Container.RemoveContainerByID(RemoveContainerByIDContext, cont.ID, map[string]interface{}{"force": true})
		if removeContainerErr != nil {
			// It's possible we're trying to remove the container when it's already being removed
			// RUNNING -> STOPPED -> RUNNING
			// It's also possible the container does not exist yet, if it's the first time you're building the app
			if !errdefs.IsContainerRemovalAlreadyInProgress(removeContainerErr) && !errdefs.IsContainerNotFound(removeContainerErr) {
				return removeContainerErr
			}
		}

		waitForContainerByIDContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		_, err = sm.Container.WaitForContainerByID(waitForContainerByIDContext, cont.ID, container.WaitConditionRemoved)
		if err != nil {
			// expected behaviour, see: https://github.com/docker/docker-py/issues/2270
			// still useful, and will wait if it's still not removed
			if !errdefs.IsContainerNotFound(err) {
				return err
			}
		}

	}

	err = sm.LogManager.ClearLogHistory(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Starting %s...", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	newContainerID, err := sm.startContainer(payload, app)
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	waitForRunningContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	runningSignal, errC := sm.Container.WaitForRunning(waitForRunningContext, newContainerID, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("The app failed to start, reason: %s", err.Error()))
		options := common.Dict{"follow": true, "stdout": true, "stderr": true}
		ioReader, logsErr := sm.Container.Logs(context.Background(), newContainerID, options)
		if logsErr != nil {
			return logsErr
		}

		sm.LogManager.StreamBlocking(payload.ContainerName.Dev, common.APP, ioReader)
		return err
	case <-runningSignal:
		break
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Dev, common.APP, nil)
	if err != nil {
		return err
	}

	return nil
}

func buildProdEnvironmentVariables(defaultEnvironmentVariables []string, payloadEnvironmentVariables map[string]interface{}) []string {
	return append(defaultEnvironmentVariables, common.EnvironmentVarsToStringArray((payloadEnvironmentVariables))...)
}

const maxEnvVarSize = 20 * 1024 // 20KB - reasonable maximum for environment variables

func writeEnvironmentVariablesToFiles(appSpecificDirectory string, envVars []string) error {
	envDir := appSpecificDirectory + "/env"
	err := os.MkdirAll(envDir, os.ModePerm)
	if err != nil {
		return err
	}

	for _, envVar := range envVars {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		varName := parts[0]
		varValue := parts[1]

		filePath := fmt.Sprintf("%s/%s.txt", envDir, varName)
		err := os.WriteFile(filePath, []byte(varValue), 0644)
		if err != nil {
			return fmt.Errorf("failed to write env var %s to file: %w", varName, err)
		}
	}

	return nil
}

// filterLargeEnvVars returns only env vars that are below the size threshold
// Large env vars should only be accessed via files in /data/env/
func filterLargeEnvVars(envVars []string) []string {
	filtered := make([]string, 0, len(envVars))
	for _, envVar := range envVars {
		if len(envVar) <= maxEnvVarSize {
			filtered = append(filtered, envVar)
		}
	}
	return filtered
}

func computeMounts(stage common.Stage, appName string, config *config.Config) ([]mount.Mount, error) {
	appSpecificDirectory := config.CommandLineArguments.AppsDirectory + "/" + strings.ToLower(string(stage)) + "/" + strings.ToLower(appName)

	err := os.MkdirAll(appSpecificDirectory, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
	}

	mounts := []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   config.CommandLineArguments.AppsSharedDir,
			Target:   "/shared",
			ReadOnly: false,
		},
		{
			Type:     mount.TypeBind,
			Source:   appSpecificDirectory,
			Target:   "/data",
			ReadOnly: false,
		},
	}

	if _, err := os.Stat("/sys/bus/w1/devices"); !os.IsNotExist(err) {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   "/sys/bus/w1/devices",
			Target:   "/sys/bus/w1/devices",
			ReadOnly: false,
		})
	}

	// for nvidia
	if _, err := os.Stat("/usr/local/cuda"); !os.IsNotExist(err) {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   "/usr/local/cuda",
			Target:   "/usr/local/cuda",
			ReadOnly: false,
		})
	}

	return mounts, nil
}

func buildDefaultEnvironmentVariables(config *config.Config, payload common.TransitionPayload, environment common.Stage, app *common.App) []string {
	environmentVariables := []string{
		fmt.Sprintf("DEVICE_SERIAL_NUMBER=%s", config.ReswarmConfig.SerialNumber),
		fmt.Sprintf("ENV=%s", environment),
		fmt.Sprintf("DEVICE_KEY=%d", config.ReswarmConfig.DeviceKey),
		fmt.Sprintf("SWARM_KEY=%d", config.ReswarmConfig.SwarmKey),
		fmt.Sprintf("APP_KEY=%d", app.AppKey),
		fmt.Sprintf("APP_NAME=%s", app.AppName),
		fmt.Sprintf("DEVICE_SECRET=%s", config.ReswarmConfig.Secret),
		fmt.Sprintf("DEVICE_NAME=%s", config.ReswarmConfig.Name),
		// Rewritten for the container's vantage point: a loopback endpoint
		// (local dev) is unreachable from a bridge network.
		fmt.Sprintf("DEVICE_ENDPOINT_URL=%s", appEndpointURL(config.ReswarmConfig.DeviceEndpointURL)),
		// The domain direct device tunnels are published under. Lets SDKs
		// (getRemoteAccessUrlForPort) compose a port's public URL without
		// replicating server-side logic.
		fmt.Sprintf("TUNNEL_DOMAIN=%s", tunnelDomainForApps(config)),
	}

	// Computed at container start: an IP change is reflected on the next app
	// restart. LAN deployments are expected to use static IPs or DHCP
	// reservations.
	if lanIP := reagentnetwork.GetPrimaryLANIP(); lanIP != "" {
		environmentVariables = append(environmentVariables, fmt.Sprintf("DEVICE_LAN_IP=%s", lanIP))
	}

	if payload.InstanceKey > 0 {
		// Instance devices: apps compose the cloud-forwarded route
		// https://i<INSTANCE_KEY>-<deviceKey>-<appName>-<port>.<cloud edge>.
		environmentVariables = append(environmentVariables, fmt.Sprintf("INSTANCE_KEY=%d", payload.InstanceKey))
	}

	if config.ReswarmConfig.ReswarmBaseURL != "" {
		deviceURL := fmt.Sprintf("%s/%s/swarms/%s/settings/devices/%s", config.ReswarmConfig.ReswarmBaseURL, config.ReswarmConfig.SwarmOwnerName, config.ReswarmConfig.SwarmName, config.ReswarmConfig.Name)
		environmentVariables = append(environmentVariables, fmt.Sprintf("RESWARM_URL=%s", config.ReswarmConfig.ReswarmBaseURL))
		environmentVariables = append(environmentVariables, fmt.Sprintf("DEVICE_URL=%s", deviceURL))
	}

	return environmentVariables
}

// remotePortEnvVars builds the env vars announcing the public side of an
// app's tunneled ports. Every port rule gets the canonical
// REMOTE_PORT_FOR_<port> name (with a _CLOUD companion for the
// internet-facing cloud port an instance backend patched into the sync
// payload); a rule's custom remote_port_environment name is emitted alongside
// for backward compatibility.
func (sm *StateMachine) remotePortEnvVars(config *config.Config, appName string, portRules []common.PortForwardRule) []string {
	var envs []string
	for _, portRule := range portRules {
		names := []string{fmt.Sprintf("REMOTE_PORT_FOR_%d", portRule.Port)}
		if portRule.RemotePortEnvironment != "" {
			names = append(names, portRule.RemotePortEnvironment)
		}

		subdomain := tunnel.CreateSubdomain(tunnel.Protocol(portRule.Protocol), uint64(config.ReswarmConfig.DeviceKey), appName, portRule.Port)
		tunnelID := tunnel.CreateTunnelID(subdomain, portRule.Protocol)
		activeTunnel := sm.StateObserver.AppManager.tunnelManager.Get(tunnelID)

		for _, name := range names {
			if activeTunnel != nil {
				envs = append(envs, fmt.Sprintf("%s=%d", name, activeTunnel.Config.RemotePort))
			}

			// Payload-borne — no local tunnel object needed.
			if portRule.CloudRemotePort > 0 {
				envs = append(envs, fmt.Sprintf("%s_CLOUD=%d", name, portRule.CloudRemotePort))
			}
		}
	}
	return envs
}

func (sm *StateMachine) computeContainerConfigs(payload common.TransitionPayload, app *common.App) (*container.Config, *container.HostConfig, error) {
	config := sm.Container.GetConfig()
	systemDefaultVariables := buildDefaultEnvironmentVariables(config, payload, app.Stage, app)
	environmentVariables := buildProdEnvironmentVariables(systemDefaultVariables, payload.EnvironmentVariables)
	environmentTemplateDefaults := common.EnvironmentTemplateToStringArray(payload.EnvironmentTemplate)

	// Get app-specific directory for writing env vars
	appSpecificDirectory := config.CommandLineArguments.AppsDirectory + "/" + strings.ToLower(string(app.Stage)) + "/" + strings.ToLower(app.AppName)

	portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
	if err != nil {
		return nil, nil, err
	}

	containerName := payload.ContainerName.Prod
	if app.Stage == common.DEV {
		containerName = payload.ContainerName.Dev
	}

	// Apps run on the default bridge network with their declared ports
	// published on agent-managed host ports. Publishing (instead of the
	// former NetworkMode "host") is what keeps concurrent apps from fighting
	// over the same host port: every container may listen on its declared
	// port privately, and the agent picks a collision-free host port.
	// Computed before the environment variables so the resulting host ports
	// can be announced to the app as DEVICE_PORT_FOR_<port>.
	exposedPorts, portBindings, err := sm.computePortBindings(payload, portRules, containerName)
	if err != nil {
		return nil, nil, err
	}
	devicePortEnvs := devicePortEnvsFromBindings(portBindings)

	var containerConfig container.Config

	if app.Stage == common.DEV {
		// Write all environment variables to files
		allEnvVars := append(systemDefaultVariables, environmentTemplateDefaults...)
		allEnvVars = append(allEnvVars, devicePortEnvs...)
		err := writeEnvironmentVariablesToFiles(appSpecificDirectory, allEnvVars)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to write environment variables to files: %w", err)
		}

		// Only pass small env vars to container, large ones are available in /data/env/
		containerEnvVars := filterLargeEnvVars(allEnvVars)

		containerConfig = container.Config{
			Image:        payload.RegistryImageName.Dev,
			Env:          containerEnvVars,
			Labels:       map[string]string{"real": "True"},
			Volumes:      map[string]struct{}{},
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,
			Tty:          true,
		}
	} else {

		fullImageNameWithTag := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, app.Version)
		getImageContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		_, err := sm.Container.GetImage(getImageContext, payload.RegistryImageName.Prod, payload.PresentVersion)
		if err != nil {
			if errdefs.IsImageNotFound(err) {
				log.Error().Msgf("Image %s:%s was not found, pulling......", payload.RegistryImageName.Prod, payload.PresentVersion)
				pullErr := sm.pullApp(payload, app)
				if pullErr != nil {
					return nil, nil, pullErr
				}
			}
		}

		var missingDefaultEnvs []string
		for _, templateEnvString := range environmentTemplateDefaults {
			envStringSplit := strings.Split(templateEnvString, "=")
			environmentName := envStringSplit[0]

			found := false
			for _, envVariableString := range environmentVariables {
				if strings.Contains(envVariableString, environmentName) {
					found = true
				}
			}

			if !found {
				missingDefaultEnvs = append(missingDefaultEnvs, templateEnvString)
			}
		}

		remotePortEnvs := sm.remotePortEnvVars(config, app.AppName, portRules)

		// Write all environment variables to files
		allEnvVars := append(environmentVariables, append(remotePortEnvs, missingDefaultEnvs...)...)
		allEnvVars = append(allEnvVars, devicePortEnvs...)
		err = writeEnvironmentVariablesToFiles(appSpecificDirectory, allEnvVars)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to write environment variables to files: %w", err)
		}

		// Only pass small env vars to container, large ones are available in /data/env/
		containerEnvVars := filterLargeEnvVars(allEnvVars)

		containerConfig = container.Config{
			Image:  fullImageNameWithTag,
			Env:    containerEnvVars,
			Labels: map[string]string{"real": "True"},
			Tty:    true,
		}
	}

	mounts, err := computeMounts(app.Stage, app.AppName, config)
	if err != nil {
		return nil, nil, err
	}

	containerConfig.ExposedPorts = exposedPorts

	hostConfig := container.HostConfig{
		// CapDrop: []string{"NET_ADMIN"},
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
		Mounts: mounts,
		Resources: container.Resources{
			Devices: []container.DeviceMapping{
				{
					PathOnHost:      "/dev",
					PathInContainer: "/dev",
				},
			},
		},
		Privileged:   true,
		NetworkMode:  "bridge",
		PortBindings: portBindings,
		ExtraHosts:   []string{hostGatewayEntry},
		CapAdd:       []string{"ALL"},
	}

	if system.HasNvidiaGPU() {
		log.Debug().Msgf("Detected a NVIDIA GPU, will request NVIDIA Device capabilities...")
		hostConfig.Runtime = "nvidia"
		// hostConfig.DeviceRequests = []container.DeviceRequest{
		// 	{
		// 		Driver: "nvidia",
		// 		Count:  -1,
		// 		Capabilities: [][]string{
		// 			{
		// 				"compute", "compat32", "graphics", "utility", "video", "display",
		// 			},
		// 		},
		// 	},
		// }
	}

	return &containerConfig, &hostConfig, nil
}

func (sm *StateMachine) createContainer(payload common.TransitionPayload, app *common.App, cConfig *container.Config, hConfig *container.HostConfig) (string, error) {
	var containerID string
	var containerName string

	if app.Stage == common.PROD {
		containerName = payload.ContainerName.Prod
	} else {
		containerName = payload.ContainerName.Dev
	}

	getContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	cont, err := sm.Container.GetContainer(getContainerContext, containerName)
	if err == nil && sm.containerNetworkConfigOutdated(cont, hConfig, containerName) {
		// Network mode and port bindings are immutable on an existing
		// container: recreate to apply them (migrates pre-managed-port
		// containers off host networking, and picks up reassigned ports).
		log.Info().Str("container", containerName).Msg("Recreating container to apply updated network/port configuration")

		removeContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()
		err = sm.Container.RemoveContainerByID(removeContainerContext, cont.ID, map[string]interface{}{"force": true})
		if err != nil && !errdefs.IsContainerNotFound(err) {
			return "", err
		}

		waitForRemovalContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()
		_, err = sm.Container.WaitForContainerByID(waitForRemovalContext, cont.ID, container.WaitConditionRemoved)
		if err != nil && !errdefs.IsContainerNotFound(err) {
			return "", err
		}

		err = errdefs.ContainerNotFound(errors.New("container was recreated"))
	}
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return "", err
		}

		createContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()
		containerID, err = sm.Container.CreateContainer(createContainerContext, *cConfig, *hConfig, network.NetworkingConfig{}, containerName)
		if err != nil {
			if errdefs.IsImageNotFound(err) && app.Stage == common.DEV {
				imageNotFoundMessage := "The image " + payload.RegistryImageName.Dev + " was not found on the device, try building the app again..."
				sm.LogManager.Write(containerName, imageNotFoundMessage)
			}
			if strings.Contains(err.Error(), "nvidia") {
				log.Debug().Msgf("Failed to launch container with NVIDIA Capabilities, retrying without... %s \n", err.Error())

				removeContainerByIdContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
				defer cancel()
				sm.Container.RemoveContainerByID(removeContainerByIdContext, containerID, map[string]interface{}{"force": true})

				// remove NVIDIA host configuration
				hConfig.Runtime = ""
				hConfig.DeviceRequests = []container.DeviceRequest{}

				createContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
				defer cancel()
				containerID, err = sm.Container.CreateContainer(createContainerContext, *cConfig, *hConfig, network.NetworkingConfig{}, containerName)
				if err != nil {
					return "", err
				}
			} else {
				return "", err
			}
		}
	} else {
		containerID = cont.ID
	}

	return containerID, nil
}

func (sm *StateMachine) startContainer(payload common.TransitionPayload, app *common.App) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		containerID, err := sm.startContainerOnce(payload, app)
		if err == nil {
			return containerID, nil
		}
		lastErr = err

		// The registry probes ports before handing them out, but a process
		// outside the agent can still grab one between probe and bind. Pick
		// fresh ports and recreate; createContainer notices the binding
		// mismatch and recreates the container.
		if !isPortAllocationError(err) || !sm.StateObserver.AppManager.reassignPortsAfterBindConflict(payload, err) {
			return "", err
		}
		log.Warn().Str("app", payload.AppName).Msg("Retrying container start with freshly assigned host ports")
	}
	return "", lastErr
}

func (sm *StateMachine) startContainerOnce(payload common.TransitionPayload, app *common.App) (string, error) {
	containerConfig, hostConfig, err := sm.computeContainerConfigs(payload, app)
	if err != nil {
		return "", err
	}

	containerID, err := sm.createContainer(payload, app, containerConfig, hostConfig)
	if err != nil {
		return "", err
	}

	startContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	err = sm.Container.StartContainer(startContainerContext, containerID)
	if err != nil {
		if strings.Contains(err.Error(), "nvidia") {
			log.Debug().Msgf("Failed to launch container with NVIDIA Capabilities, retrying without... %s \n", err.Error())

			removeContainerByIdContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
			defer cancel()
			sm.Container.RemoveContainerByID(removeContainerByIdContext, containerID, map[string]interface{}{"force": true})

			// remove nvidia device request
			hostConfig.DeviceRequests = []container.DeviceRequest{}

			containerID, err = sm.createContainer(payload, app, containerConfig, hostConfig)
			if err != nil {
				return "", err
			}

			startContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
			defer cancel()

			err = sm.Container.StartContainer(startContainerContext, containerID)
			if err != nil {
				return "", err
			}

		} else if isPortAllocationError(err) {
			// Surface host port conflicts so startContainer can reassign
			// ports and retry.
			return "", err
		}
	}

	return containerID, nil
}
