package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/logging"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
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

	fullImageNameWithTag := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.PresentVersion)

	_, err := sm.Container.GetImage(ctx, payload.RegistryImageName.Prod, payload.PresentVersion)
	if err != nil {
		if errdefs.IsImageNotFound(err) {
			err := sm.setState(app, common.DOWNLOADING)
			if err != nil {
				return err
			}

			pullErr := sm.pullApp(payload, app)
			if err != nil {
				return pullErr
			}
		}
	}

	containerConfig := container.Config{
		Image:   fullImageNameWithTag,
		Env:     []string{},
		Labels:  map[string]string{"real": "True"},
		Volumes: map[string]struct{}{},
		Tty:     true,
	}

	hostConfig := container.HostConfig{
		// CapDrop: []string{"NET_ADMIN"},
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
		Privileged:  true,
		NetworkMode: "host",
		CapAdd:      []string{"ALL"},
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
		} else {
			containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Prod)
			if err != nil {
				return err
			}
		}
	} else {
		containerID = cont.ID
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	// in case there's an active subscription, (an open panel) before the app was started, we need to make sure we start a stream
	// NOTE: this is currently not possible since we close the panel everytime we switch states for production apps
	// but in case we change this in the future, then this is already accounted for
	err = sm.LogManager.UpdateLogStream(payload.ContainerName.Prod)
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

	hostConfig := container.HostConfig{
		// CapDrop: []string{"NET_ADMIN"},
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
		Privileged:  true,
		NetworkMode: "host",
		CapAdd:      []string{"ALL"},
	}

	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		err := sm.setState(app, common.STOPPING)
		if err != nil {
			return err
		}

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
			return err
		}

	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	newContainerID, err := sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNameAlreadyInUse(err) {
			return err
		}
	}

	err = sm.Container.StartContainer(ctx, newContainerID)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	// when we build the app, we create a subscription
	// now we make sure to populate this existing subscription with a stream (if the user is still subscribed)
	// since we have now started the app
	// it's also possible for users to have an active subscription before the app has started running
	err = sm.LogManager.UpdateLogStream(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	return nil
}

func buildDefaultEnvironmentVariables(config *config.Config, environment common.Stage) []string {
	return []string{
		fmt.Sprintf("SERIAL_NUMBER=%s", config.ReswarmConfig.SerialNumber),
		fmt.Sprintf("ENV=%s", environment),
		fmt.Sprintf("DEVICE_KEY=%s", config.ReswarmConfig.DeviceKey),
		fmt.Sprintf("SWARM_KEY=%s", config.ReswarmConfig.SwarmKey),
		fmt.Sprintf("DEVICE_SECRET=%s", config.ReswarmConfig.Secret),
		fmt.Sprintf("DEVICE_ENDPOINT_URL=%s", config.ReswarmConfig.DeviceEndpointURL),
	}
}
