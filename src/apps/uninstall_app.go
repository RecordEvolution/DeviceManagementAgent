package apps

import (
	"reagent/common"
)

func (sm *StateMachine) uninstallApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.removeApp(payload, app)
	if err != nil {
		return err
	}

	return sm.setState(app, common.UNINSTALLED)
}
