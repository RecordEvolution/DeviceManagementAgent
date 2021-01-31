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

func (sm *StateMachine) runApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	if payload.Stage == common.DEV {
		sm.runDevApp(payload, app, errorChannel)
	}
}

func (sm *StateMachine) runDevApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
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
			errorChannel <- err
			return
		}
	} else {
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, containerID, map[string]interface{}{"force": true})
		if removeContainerErr != nil {
			errorChannel <- removeContainerErr
			return
		}
	}

	containerID, err = sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName)
	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.Container.StartContainer(ctx, containerID)
	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.setState(app, common.STARTING)
	sm.Container.WaitForContainer(ctx, containerID, container.WaitConditionNotRunning)

	err = sm.setState(app, common.RUNNING)
	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.LogManager.Write(payload.ContainerName, logging.BUILD, fmt.Sprintf("Now running app %s", payload.AppName))
	if err != nil {
		errorChannel <- err
		return
	}
}
