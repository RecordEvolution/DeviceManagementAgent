package apps

import "reagent/common"

func (sm *StateMachine) recoverFailToRunningHandler(payload common.TransitionPayload, app *common.App) error {
	return sm.runApp(payload, app)
}
