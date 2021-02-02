package apps

import (
	"context"
	"errors"
	"reagent/common"
)

func (sm *StateMachine) stopBuild(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.stopDevApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) stopDevBuild(payload common.TransitionPayload, app *common.App) error {
	id := sm.LogManager.GetActiveBuildId(payload.ContainerName.Dev)
	if id != "" {
		ctx := context.Background()
		err := sm.Container.CancelBuild(ctx, id)
		if err != nil {
			return err
		}
	}

	return errors.New("active build with id " + id + " was not found")
}
