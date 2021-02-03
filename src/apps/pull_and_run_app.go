package apps

import (
	"reagent/common"
)

func (sm *StateMachine) pullAndRunApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		err := sm.pullApp(payload, app)
		if err != nil {
			return err
		}

		err = sm.runApp(payload, app)
		if err != nil {
			return err
		}
	}

	return nil
}
