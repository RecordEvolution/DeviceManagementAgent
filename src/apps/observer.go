package apps

import (
	"fmt"
)

type StateObserver struct {
	StateStorer StateStorer
}

func (so *StateObserver) Notify(app *App, achievedState AppState) {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.StateStorer.UpdateAppState(app, achievedState)
	if err != nil {
		panic(err)
	}
	fmt.Println()
	fmt.Println()
	fmt.Printf("app: %+v, achieved state: %s", app, achievedState)
}
