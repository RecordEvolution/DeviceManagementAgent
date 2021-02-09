package apps

import (
	"reagent/common"
)

func (sm *StateMachine) stopBuild(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.stopDevApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) stopDevBuild(payload common.TransitionPayload, app *common.App) error {
	// TODO: implement

	return nil
}
