package apps

import (
	"reagent/common"
)

func (sm *StateMachine) removedToPresent(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.noActionTransitionFunc(payload, app)
	}

	return sm.pullApp(payload, app)
}
