package apps

import (
	"fmt"
)

type StateObserver struct {
	stateStorer StateStorer
}

func (so *StateObserver) Notify(app *App, achievedState AppState) {
	// doublecheck if state is actually achievable and set the state in the database
	so.stateStorer.UpdateAppState(app, achievedState)
	fmt.Printf("app: %+v, achieved state: %s", app, achievedState)
}
