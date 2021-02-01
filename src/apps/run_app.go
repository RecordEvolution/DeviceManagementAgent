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
		err := sm.runDevApp(payload, app)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateMachine) runDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	containerConfig := container.Config{
		Image:   payload.RepositoryImageName,
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

	containerID, err := sm.Container.GetContainerID(ctx, payload.ContainerName)
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

	containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName)
	if err != nil {
		return err
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	sm.Container.WaitForContainer(ctx, containerID, container.WaitConditionNotRunning)

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		return err
	}

	return nil
}
