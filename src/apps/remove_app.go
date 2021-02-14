package apps

import (
	"context"
	"reagent/common"
	"reagent/errdefs"

	"github.com/docker/docker/api/types/container"
)

func (sm *StateMachine) removeApp(payload common.TransitionPayload, app *common.App) error {
	if app.Stage == common.PROD {
		return sm.removeProdApp(payload, app)
	} else if app.Stage == common.DEV {
		return sm.removeDevApp(payload, app)
	}

	return nil
}

func (sm *StateMachine) removeDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

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

	return sm.setState(app, common.REMOVED)
}

func (sm *StateMachine) removeProdApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

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

	return sm.setState(app, common.REMOVED)
}
