package apps

import (
	"reagent/common"
	"reagent/errdefs"
)

func (sm *StateMachine) removedToPresent(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return errdefs.NoActionTransition()
	}

	return sm.pullApp(payload, app)
}
