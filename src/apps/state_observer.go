package apps

import (
	"reagent/api/common"
)

type StateObserver struct {
	StateStorer StateStorer
	StateSyncer StateSyncer
}

func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.StateStorer.UpdateAppState(app, achievedState)
	if err != nil {
		return err
	}

	err = so.StateSyncer.Sync(app, achievedState)
	if err != nil {
		return err
	}

	return nil
}
