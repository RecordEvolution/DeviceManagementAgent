package apps

import (
	"reagent/api/common"
)

type StateObserver struct {
	StateUpdater StateUpdater
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
