package apps

import "reagent/common"

func (sm *StateMachine) recoverFailToRunningHandler(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		err := sm.buildApp(payload, app)
		if err != nil {
			return err
		}

		err = sm.runApp(payload, app)
		if err != nil {
			return err
		}

	} else if payload.Stage == common.PROD {
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
