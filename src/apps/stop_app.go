package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) stopApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.stopDevApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) stopDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	err := sm.Container.StopContainerByName(ctx, payload.ContainerName.Dev, 0)

	if err != nil {
		return err
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}
