package apps

import (
	"context"
	"fmt"
	"os"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
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
	ctx := context.Background()

	fullImageNameWithTag := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, app.Version)
	_, err := sm.Container.GetImage(ctx, payload.RegistryImageName.Prod, payload.PresentVersion)
	if err != nil {
		if errdefs.IsImageNotFound(err) {
			log.Error().Msgf("Image %s:%s was not found, pulling......", payload.RegistryImageName.Prod, payload.PresentVersion)
			pullErr := sm.pullApp(payload, app)
			if pullErr != nil {
				return pullErr
			}
		}
	}

	config := sm.Container.GetConfig()
	defaultEnvironmentVariables := buildDefaultEnvironmentVariables(config, app.Stage)
	environmentVariables := buildProdEnvironmentVariables(defaultEnvironmentVariables, payload.EnvironmentVariables)

	containerConfig := container.Config{
		Image:  fullImageNameWithTag,
		Env:    environmentVariables,
		Labels: map[string]string{"real": "True"},
		Tty:    true,
	}

	mounts, err := computeMounts(app.Stage, app.AppName, config)
	if err != nil {
		return err
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

	err = sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
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

	var containerID string
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
		containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Prod)
		if err != nil {
			return err
		}
	} else {
		containerID = cont.ID
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	runningSignal, errC := sm.Container.WaitForRunning(ctx, containerID, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		sm.LogManager.Write(payload.ContainerName.Dev, "The app has failed to start.")
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

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Prod, common.APP, nil)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) runDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	config := sm.Container.GetConfig()
	defaultEnvironmentVariables := buildDefaultEnvironmentVariables(config, app.Stage)

	containerConfig := container.Config{
		Image:        payload.RegistryImageName.Dev,
		Env:          defaultEnvironmentVariables,
		Labels:       map[string]string{"real": "True"},
		Volumes:      map[string]struct{}{},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		Tty:          true,
	}

	mounts, err := computeMounts(app.Stage, app.AppName, config)
	if err != nil {
		return err
	}

	hostConfig := container.HostConfig{
		// CapDrop: []string{"NET_ADMIN"},
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
		Privileged:  true,
		NetworkMode: "host",
		Mounts:      mounts,
		Resources:   computeResources(),
		CapAdd:      []string{"ALL"},
	}

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

	newContainerID, err := sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Dev)
	if err != nil {
		if errdefs.IsImageNotFound(err) {
			imageNotFoundMessage := "The image " + payload.RegistryImageName.Dev + " was not found on the device, try building the app again..."
			sm.LogManager.Write(payload.ContainerName.Dev, imageNotFoundMessage)
		}
		return err
	}

	err = sm.Container.StartContainer(ctx, newContainerID)
	if err != nil {
		return err
	}

	pollingRate := time.Second * 1
	runningSignal, errC := sm.Container.WaitForRunning(ctx, newContainerID, pollingRate)

	// block and wait for running, if exited status then return as a failed state
	select {
	case err = <-errC:
		sm.LogManager.Write(payload.ContainerName.Dev, "The app has failed to start.")
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
		{
			Type:     mount.TypeBind,
			Source:   "/etc/resolv.conf",
			Target:   "/etc/resolv.conf",
			ReadOnly: false,
		},
	}

	// path to this exists
	if _, err := os.Stat("/sys/bus/w1/devices"); !os.IsNotExist(err) {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   "/sys/bus/w1/devices",
			Target:   "/sys/bus/w1/devices",
			ReadOnly: false,
		})
	}

	return mounts, nil
}

func computeResources() container.Resources {
	return container.Resources{
		Devices: []container.DeviceMapping{
			{
				PathOnHost:      "/dev",
				PathInContainer: "/dev",
			},
		},
	}
}

func buildDefaultEnvironmentVariables(config *config.Config, environment common.Stage) []string {
	environmentVariables := []string{
		fmt.Sprintf("DEVICE_SERIAL_NUMBER=%s", config.ReswarmConfig.SerialNumber),
		fmt.Sprintf("ENV=%s", environment),
		fmt.Sprintf("DEVICE_KEY=%d", config.ReswarmConfig.DeviceKey),
		fmt.Sprintf("SWARM_KEY=%d", config.ReswarmConfig.SwarmKey),
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
