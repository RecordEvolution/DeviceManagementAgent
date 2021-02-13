package apps

import "reagent/common"

func (sm *StateMachine) recoverFailToPresentHandler(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.buildApp(payload, app)
	}

	return sm.pullApp(payload, app)
}
