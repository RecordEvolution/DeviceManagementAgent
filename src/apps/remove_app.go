package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/errdefs"

	"github.com/docker/docker/api/types/container"
)

func (sm *StateMachine) removeApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.removeComposeApp(payload, app)
	}

	if app.Stage == common.PROD {
		return sm.removeProdApp(payload, app)
	} else if app.Stage == common.DEV {
		return sm.removeDevApp(payload, app)
	}

	return nil
}

func (sm *StateMachine) removeComposeApp(payload common.TransitionPayload, app *common.App) error {
	containerName := payload.ContainerName.Dev
	registeryImageName := payload.RegistryImageName.Dev
	if payload.Stage == common.PROD {
		registeryImageName = payload.RegistryImageName.Prod
		containerName = payload.ContainerName.Prod
	}

	err := sm.LogManager.ClearLogHistory(containerName)
	if err != nil {
		return err
	}

	removeInitMessage := fmt.Sprintf("Starting removal process for %s...", payload.AppName)
	err = sm.LogManager.Write(containerName, removeInitMessage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.DELETING)
	if err != nil {
		return err
	}

	options := map[string]interface{}{"force": true}

	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

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

	ctx := context.Background()
	err = sm.Container.RemoveImagesByName(ctx, registeryImageName+"*", options)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.REMOVED)
	if err != nil {
		return err
	}

	sucessRemoveMessage := fmt.Sprintf("Successfully removed %s!", payload.AppName)

	return sm.LogManager.Write(containerName, sucessRemoveMessage)
}

func (sm *StateMachine) removeDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	removeInitMessage := fmt.Sprintf("Starting removal process for %s...", payload.AppName)
	err = sm.LogManager.Write(payload.ContainerName.Dev, removeInitMessage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.DELETING)
	if err != nil {
		return err
	}

	options := map[string]interface{}{"force": true}
	// check if the image has a running container
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		// remove container if it exists
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, cont.ID, options)
		if removeContainerErr != nil {
			if !errdefs.IsContainerNotFound(removeContainerErr) {
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

	err = sm.Container.RemoveImagesByName(ctx, payload.RegistryImageName.Dev, options)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.REMOVED)
	if err != nil {
		return err
	}

	sucessRemoveMessage := fmt.Sprintf("Successfully removed %s!", payload.AppName)
	return sm.LogManager.Write(payload.ContainerName.Dev, sucessRemoveMessage)
}

func (sm *StateMachine) removeProdApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	removeInitMessage := fmt.Sprintf("Starting removal process for %s...", payload.AppName)
	err = sm.LogManager.Write(payload.ContainerName.Prod, removeInitMessage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.DELETING)
	if err != nil {
		return err
	}

	options := map[string]interface{}{"force": true}

	// check if the image has a running container
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	} else {
		// remove container if it exists
		removeContainerErr := sm.Container.RemoveContainerByID(ctx, cont.ID, options)
		if removeContainerErr != nil {
			if !errdefs.IsContainerNotFound(removeContainerErr) {
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

	err = sm.Container.RemoveImagesByName(ctx, payload.RegistryImageName.Prod, options)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.REMOVED)
	if err != nil {
		return err
	}

	sucessRemoveMessage := fmt.Sprintf("Successfully removed %s!", payload.AppName)
	return sm.LogManager.Write(payload.ContainerName.Prod, sucessRemoveMessage)
}
