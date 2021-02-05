package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
)

func (sm *StateMachine) removeApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		ctx := context.Background()

		options := map[string]interface{}{"force": true}

		// check if the image has a running container
		cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
		fmt.Println("found ", cont.ID)
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

			// doesn't seem to work
			// _, err := sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionRemoved)
			// if err != nil {
			// 	if !errdefs.IsContainerNotFound(err) {
			// 		return err
			// 	}
			// }
		}

		err = sm.Container.RemoveImagesByName(ctx, payload.RegistryImageName.Prod, options)
		if err != nil {
			return err
		}

		sm.setState(app, common.REMOVED)
	}

	return nil
}
