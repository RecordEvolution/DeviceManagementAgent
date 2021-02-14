package apps

import (
	"reagent/common"
)

type StateObserver struct {
	StateUpdater *StateUpdater
}

// Notify verifies a changed state in the StateMachine and stores it in the database
func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.StateUpdater.UpdateAppState(app, achievedState)
	if err != nil {
		return err
	}
	return nil
}

// NotifyLocalOnly is used to update the local app state with the remote app state
func (so *StateObserver) NotifyLocalOnly(app *common.App, achievedState common.AppState) error {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.StateUpdater.UpdateLocalAppState(app, achievedState)
	if err != nil {
		return err
	}
	return nil
}
