package apps

import (
	"errors"
	"reagent/common"
)

func (sm *StateMachine) getUpdateTransition(payload common.TransitionPayload, app *common.App) TransitionFunc {
	return sm.updateApp
}

func (sm *StateMachine) updateApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return errors.New("cannot update dev app")
	}

	// Stop running containers of the app + remove all old images
	err := sm.removeProdApp(payload, app)
	if err != nil {
		return err
	}

	// Pull newest image of app
	err = sm.pullApp(payload, app)
	if err != nil {
		return err
	}

	// The state validation will ensure it will reach it's requestedState again
	return nil
}
