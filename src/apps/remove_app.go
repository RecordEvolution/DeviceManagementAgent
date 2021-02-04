package apps

import (
	"context"
	"reagent/common"
	"reagent/errdefs"
)

func (sm *StateMachine) removeApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		ctx := context.Background()

		options := map[string]interface{}{"force": true}

		// check if the image has a running container
		container, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
		if err != nil {
			if !errdefs.IsContainerNotFound(err) {
				return err
			}
		} else {
			// remove container if it exists
			removeContainerErr := sm.Container.RemoveContainerByID(ctx, container.ID, options)
			if removeContainerErr != nil {
				return removeContainerErr
			}
		}

		err = sm.Container.RemoveImagesByName(ctx, payload.RegistryImageName.Prod, options)
		if err != nil {
			return err
		}

		sm.setState(app, common.REMOVED)
	}

	return nil
}
