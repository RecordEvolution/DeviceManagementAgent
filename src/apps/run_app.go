package apps

import (
	"context"
	"fmt"
	"reagent/common"
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

	fmt.Printf("%+v \n", app)

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

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) runDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	containerConfig := container.Config{
		Image:   payload.RegistryImageName.Dev,
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
			if !errdefs.IsContainerRemovalAlreadyInProgress(removeContainerErr) {
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
