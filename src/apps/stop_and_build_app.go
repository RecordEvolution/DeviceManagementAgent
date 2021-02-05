package apps

import "reagent/common"

func (sm *StateMachine) stopAndBuildApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.stopDevApp(payload, app)
	if err != nil {
		return err
	}

	err = sm.buildDevApp(payload, app, false)
	if err != nil {
		return err
	}
	return nil
}
