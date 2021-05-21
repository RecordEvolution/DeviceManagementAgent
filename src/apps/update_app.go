package apps

import (
	"context"
	"reagent/common"
	"reagent/errdefs"
	"time"

	"github.com/pkg/errors"
)

func (sm *StateMachine) getUpdateTransition(payload common.TransitionPayload, app *common.App) TransitionFunc {
	return sm.updateApp
}

func (sm *StateMachine) updateApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return errors.New("cannot update dev app")
	}

	ctx := context.Background()
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
	if err == nil {
		err = sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
		if err != nil {
			return err
		}

		// should return 'container not found' error, this way we know it's removed successfully
		_, errC := sm.Container.PollContainerState(ctx, cont.ID, time.Second)
		select {
		case err := <-errC:
			if !errdefs.IsContainerNotFound(err) {
				return err
			}
		}
	}

	// Pull newest image of app
	err = sm.pullApp(payload, app)
	if err != nil {
		return err
	}

	err = sm.Container.RemoveImageByName(ctx, payload.RegistryImageName.Prod, payload.PresentVersion, map[string]interface{}{"force": true})
	if err != nil {
		return err
	}

	// update the version of the local requested states
	payload.NewestVersion = app.Version
	payload.PresentVersion = app.Version
	payload.Version = app.Version

	// The state validation will ensure it will reach it's requestedState again
	return sm.StateObserver.AppStore.UpdateLocalRequestedState(payload)
}
