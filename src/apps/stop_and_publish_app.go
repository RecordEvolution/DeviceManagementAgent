package apps

import "reagent/common"

func (sm *StateMachine) removeAndPublishApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		err := sm.removeDevApp(payload, app)
		if err != nil {
			return err
		}

		err = sm.publishApp(payload, app)
		if err != nil {
			return err
		}
	}

	return nil
}
