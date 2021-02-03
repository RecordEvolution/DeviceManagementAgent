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
	fmt.Println("Running run app as", payload.Stage)
	if payload.Stage == common.DEV {
		return sm.runDevApp(payload, app)
	} else if payload.Stage == common.PROD {
		return sm.runProdApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) runProdApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	fullImageNameWithTag := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.Version)

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

	containerID, err := sm.Container.GetContainerID(ctx, payload.ContainerName.Prod)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		} else {
			containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Prod)
			if err != nil {
				return err
			}
		}
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	// TODO: handle error channel for wait
	sm.Container.WaitForContainer(ctx, containerID, container.WaitConditionNotRunning)

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
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

	containerID, err := sm.Container.GetContainerID(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, containerID, map[string]interface{}{"force": true})
		if removeContainerErr != nil {
			return removeContainerErr
		}
	}

	containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	// TODO: handle error channel for wait
	sm.Container.WaitForContainer(ctx, containerID, container.WaitConditionNotRunning)

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	return nil
}
