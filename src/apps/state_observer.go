package apps

import (
	"reagent/api/common"
)

type StateObserver struct {
	StateStorer  StateStorer
	StateUpdater StateSyncer
}

func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.StateStorer.UpdateAppState(app, achievedState)
	if err != nil {
		panic(err)
	}
	err = so.StateUpdater.Sync(app, achievedState)
	if err != nil {
		panic(err)
	}
}
