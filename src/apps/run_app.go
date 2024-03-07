package apps

import (
	"context"
	"fmt"
	"os"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
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

	ctx := context.Background()

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
	runningSignal, errC := sm.Container.WaitForRunning(ctx, containerID, pollingRate)

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

	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	_, _, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, _, cmd, err = compose.Remove(dockerComposePath)
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

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	_, stdErrChan, upCmd, err := compose.Up(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(stdErrChan, payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = upCmd.Wait()
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	runningSignal, _ := compose.WaitForRunning(dockerComposePath, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	<-runningSignal

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Now running app %s (%s)", payload.AppName, app.Version))
	if err != nil {
		return err
	}

	logsChannel, err := compose.Logs(dockerComposePath)
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

	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	_, _, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, _, cmd, err = compose.Remove(dockerComposePath)
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

	_, stdErrChan, upCmd, err := compose.Up(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(stdErrChan, payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	err = upCmd.Wait()
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	runningSignal, _ := compose.WaitForRunning(dockerComposePath, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	<-runningSignal

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Now running app %s (%s)", payload.AppName, app.Version))
	if err != nil {
		return err
	}

	logsChannel, err := compose.Logs(dockerComposePath)
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
	ctx := context.Background()

	if payload.DockerCompose != nil {
		return sm.runDevComposeApp(payload, app)
	}

	// remove old container first, if it exists
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
		if removeContainerErr != nil {
			// It's possible we're trying to remove the container when it's already being removed
			// RUNNING -> STOPPED -> RUNNING
			// It's also possible the container does not exist yet, if it's the first time you're building the app
			if !errdefs.IsContainerRemovalAlreadyInProgress(removeContainerErr) && !errdefs.IsContainerNotFound(removeContainerErr) {
				return removeContainerErr
			}
		}

		_, err = sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionRemoved)
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
	runningSignal, errC := sm.Container.WaitForRunning(ctx, newContainerID, pollingRate)

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

func computeMounts(stage common.Stage, appName string, config *config.Config) ([]mount.Mount, error) {
	appSpecificDirectory := strings.ToLower(config.CommandLineArguments.AppsDirectory + "/" + string(stage) + "/" + appName)

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

func buildDefaultEnvironmentVariables(config *config.Config, environment common.Stage, appKey uint64) []string {
	environmentVariables := []string{
		fmt.Sprintf("DEVICE_SERIAL_NUMBER=%s", config.ReswarmConfig.SerialNumber),
		fmt.Sprintf("ENV=%s", environment),
		fmt.Sprintf("DEVICE_KEY=%d", config.ReswarmConfig.DeviceKey),
		fmt.Sprintf("SWARM_KEY=%d", config.ReswarmConfig.SwarmKey),
		fmt.Sprintf("APP_KEY=%d", appKey),
		fmt.Sprintf("DEVICE_SECRET=%s", config.ReswarmConfig.Secret),
		fmt.Sprintf("DEVICE_NAME=%s", config.ReswarmConfig.Name),
		fmt.Sprintf("DEVICE_ENDPOINT_URL=%s", config.ReswarmConfig.DeviceEndpointURL),
	}

	if config.ReswarmConfig.ReswarmBaseURL != "" {
		deviceURL := fmt.Sprintf("%s/%s/swarms/%s/devices/device/%s", config.ReswarmConfig.ReswarmBaseURL, config.ReswarmConfig.SwarmOwnerName, config.ReswarmConfig.SwarmName, config.ReswarmConfig.Name)
		environmentVariables = append(environmentVariables, fmt.Sprintf("RESWARM_URL=%s", config.ReswarmConfig.ReswarmBaseURL))
		environmentVariables = append(environmentVariables, fmt.Sprintf("DEVICE_URL=%s", deviceURL))
	}

	return environmentVariables
}

func (sm *StateMachine) computeContainerConfigs(payload common.TransitionPayload, app *common.App) (*container.Config, *container.HostConfig, error) {
	ctx := context.Background()

	config := sm.Container.GetConfig()
	systemDefaultVariables := buildDefaultEnvironmentVariables(config, app.Stage, app.AppKey)
	environmentVariables := buildProdEnvironmentVariables(systemDefaultVariables, payload.EnvironmentVariables)
	environmentTemplateDefaults := common.EnvironmentTemplateToStringArray(payload.EnvironmentTemplate)

	var containerConfig container.Config

	if app.Stage == common.DEV {
		containerConfig = container.Config{
			Image:        payload.RegistryImageName.Dev,
			Env:          append(systemDefaultVariables, environmentTemplateDefaults...),
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
		_, err := sm.Container.GetImage(ctx, payload.RegistryImageName.Prod, payload.PresentVersion)
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

		var remotePortEnvs []string
		portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
		if err != nil {
			return nil, nil, err
		}

		for _, portRule := range portRules {
			if portRule.RemotePortEnvironment != "" {
				subdomain := tunnel.CreateSubdomain(tunnel.Protocol(portRule.Protocol), uint64(config.ReswarmConfig.DeviceKey), app.AppName, portRule.Port)
				tunnelID := tunnel.CreateTunnelID(subdomain, portRule.Protocol)
				tunnel := sm.StateObserver.AppManager.tunnelManager.Get(tunnelID)

				if tunnel != nil {
					portEnv := fmt.Sprintf("%s=%d", portRule.RemotePortEnvironment, tunnel.Config.RemotePort)
					remotePortEnvs = append(remotePortEnvs, portEnv)
				}
			}
		}

		containerConfig = container.Config{
			Image:  fullImageNameWithTag,
			Env:    append(environmentVariables, append(remotePortEnvs, missingDefaultEnvs...)...),
			Labels: map[string]string{"real": "True"},
			Tty:    true,
		}
	}

	mounts, err := computeMounts(app.Stage, app.AppName, config)
	if err != nil {
		return nil, nil, err
	}

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
		Privileged:  true,
		NetworkMode: "host",
		CapAdd:      []string{"ALL"},
	}

	if system.HasNvidiaGPU() {
		log.Debug().Msgf("Detected a NVIDIA GPU, will request NVIDIA Device capabilities...\n")
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
	ctx := context.Background()

	var containerID string
	var containerName string

	if app.Stage == common.PROD {
		containerName = payload.ContainerName.Prod
	} else {
		containerName = payload.ContainerName.Dev
	}

	cont, err := sm.Container.GetContainer(ctx, containerName)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return "", err
		}

		containerID, err = sm.Container.CreateContainer(ctx, *cConfig, *hConfig, network.NetworkingConfig{}, containerName)
		if err != nil {
			if errdefs.IsImageNotFound(err) && app.Stage == common.DEV {
				imageNotFoundMessage := "The image " + payload.RegistryImageName.Dev + " was not found on the device, try building the app again..."
				sm.LogManager.Write(containerName, imageNotFoundMessage)
			}
			if strings.Contains(err.Error(), "nvidia") {
				log.Debug().Msgf("Failed to launch container with NVIDIA Capabilities, retrying without... %s \n", err.Error())

				sm.Container.RemoveContainerByID(ctx, containerID, map[string]interface{}{"force": true})

				// remove NVIDIA host configuration
				hConfig.Runtime = ""
				hConfig.DeviceRequests = []container.DeviceRequest{}

				containerID, err = sm.Container.CreateContainer(ctx, *cConfig, *hConfig, network.NetworkingConfig{}, containerName)
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
	ctx := context.Background()

	containerConfig, hostConfig, err := sm.computeContainerConfigs(payload, app)
	if err != nil {
		return "", err
	}

	containerID, err := sm.createContainer(payload, app, containerConfig, hostConfig)
	if err != nil {
		return "", err
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		if strings.Contains(err.Error(), "nvidia") {
			log.Debug().Msgf("Failed to launch container with NVIDIA Capabilities, retrying without... %s \n", err.Error())

			sm.Container.RemoveContainerByID(ctx, containerID, map[string]interface{}{"force": true})

			// remove nvidia device request
			hostConfig.DeviceRequests = []container.DeviceRequest{}

			containerID, err = sm.createContainer(payload, app, containerConfig, hostConfig)
			if err != nil {
				return "", err
			}

			err = sm.Container.StartContainer(ctx, containerID)
			if err != nil {
				return "", err
			}

		}
	}

	return containerID, nil
}
