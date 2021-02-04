package apps

import (
	"context"
	"errors"
	"fmt"
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

		// TODO: properly handle multiple versions
		version := ""
		if payload.Version != "" {
			version = payload.Version
		} else if payload.PresentVersion != "" {
			version = payload.PresentVersion
		} else if payload.NewestVersion != "" {
			version = payload.NewestVersion
		}

		fmt.Printf("%+v\n", payload)

		if version == "" {
			return errors.New("version string missing from payload")
		}

		err = sm.Container.RemoveImagesByName(ctx, payload.RegistryImageName.Prod, options)
		if err != nil {
			return err
		}

		sm.setState(app, common.REMOVED)
	}

	return nil
}
