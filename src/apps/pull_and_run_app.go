package apps

import (
	"reagent/common"
)

func (sm *StateMachine) removedToRunning(payload common.TransitionPayload, app *common.App) error {
	// handles both pulling and building when images are not found
	return sm.runApp(payload, app)
}
