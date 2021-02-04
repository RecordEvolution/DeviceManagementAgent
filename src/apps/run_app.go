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

	// TODO: properly handle multiple versions
	var version string
	if payload.NewestVersion != payload.PresentVersion {
		version = payload.NewestVersion
	} else if payload.NewestVersion != payload.Version {
		version = payload.NewestVersion
	}

	fmt.Println("Versions:", "PV:", payload.PresentVersion, "NV:", payload.NewestVersion, "V:", payload.Version)

	fullImageNameWithTag := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, version)

	_, err := sm.Container.GetImage(ctx, payload.RegistryImageName.Prod, version)
	if err != nil {
		if errdefs.IsImageNotFound(err) {
			sm.setState(app, common.DOWNLOADING)
			pullErr := sm.pullApp(payload, app)
			if err != nil {
				return pullErr
			}
		}
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
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

	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
		if removeContainerErr != nil {
			return removeContainerErr
		}
	}

	newContainerID, err := sm.Container.CreateContainer(ctx, containerConfig, hostConfig, network.NetworkingConfig{}, payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = sm.Container.StartContainer(ctx, newContainerID)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STARTING)
	if err != nil {
		return err
	}

	// TODO: handle error channel for wait
	sm.Container.WaitForContainer(ctx, newContainerID, container.WaitConditionNotRunning)

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
