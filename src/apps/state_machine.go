package apps

import (
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
	"reagent/persistence"
	"reflect"
	"runtime"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver StateObserver
	StateStorer   persistence.StateStorer
	LogManager    logging.LogManager
	Container     container.Container
	appStates     []*common.App
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     sm.pullAndRunApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.UNINSTALLED: nil,
		},
		common.UNINSTALLED: {
			common.PRESENT:   sm.pullApp,
			common.RUNNING:   nil,
			common.BUILT:     sm.buildApp,
			common.PUBLISHED: nil,
		},
		common.PRESENT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.FAILED: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     nil,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   nil,
		},
		common.BUILT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.TRANSFERED: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     nil,
		},
		common.TRANSFERING: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     nil,
		},
		common.PUBLISHED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.RUNNING: {
			common.PRESENT:     sm.stopApp,
			common.BUILT:       nil,
			common.PUBLISHED:   nil,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
		},
		common.DOWNLOADING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
		},
		common.STARTING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.STOPPING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.UPDATING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.DELETING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
	}

	return stateTransitionMap[prevState][nextState]
}

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}
	app.CurrentState = state
	return nil
}

func (sm *StateMachine) getApp(appKey uint64, stage common.Stage) *common.App {
	for i := range sm.appStates {
		state := sm.appStates[i]
		if state.AppKey == appKey && state.Stage == stage {
			return state
		}
	}
	return nil
}

func (sm *StateMachine) VerifyState(app *common.App) error {
	fmt.Printf("Verifying if app (%d, %s) is in latest state...\n", app.AppKey, app.Stage)

	requestedStatePayload, err := sm.StateStorer.GetRequestedState(app)
	if err != nil {
		return err
	}

	// TODO: what to do when the app transition fails? How do we handle that?
	if app.CurrentState == common.FAILED {
		fmt.Println("App transition finished in a failed state")
		return nil
	}

	if requestedStatePayload.RequestedState != app.CurrentState {
		fmt.Printf("App (%d, %s) is not in latest state, transitioning to %s...\n", app.AppKey, app.Stage, requestedStatePayload.RequestedState)
		return sm.RequestAppState(requestedStatePayload)
	}

	fmt.Printf("App (%d, %s) is in latest state!\n", app.AppKey, app.Stage)
	return nil
}

func (sm *StateMachine) RequestAppState(payload common.TransitionPayload) error {
	app := sm.getApp(payload.AppKey, payload.Stage)

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app = &common.App{
			AppKey:                 payload.AppKey,
			AppName:                payload.AppName,
			CurrentState:           payload.CurrentState,
			DeviceToAppKey:         payload.DeviceToAppKey,
			RequestorAccountKey:    payload.RequestorAccountKey,
			ManuallyRequestedState: payload.RequestedState,
			Stage:                  payload.Stage,
			RequestUpdate:          false,
		}
		// It is possible that there is already a current app state
		// if we receive a sync request from the remote database
		// in that case, take that one
		if payload.CurrentState == "" {
			// Set the state of the newly added app to REMOVED
			app.CurrentState = common.REMOVED
		}

		// If app does not exist in database, it will be added
		// + remote app state will be updated if it received one from the database
		// TODO: since the remote database state is already set whenever we received a currentState, we do not need to update the remote app state again
		sm.setState(app, app.CurrentState)

		sm.appStates = append(sm.appStates, app)
	}

	// If appState is already up to date we should do nothing
	// It's possible to go from a built/published state to a built/published state since both represent a present state
	if app.CurrentState == payload.RequestedState && app.CurrentState != common.BUILT && app.CurrentState != common.PUBLISHED {
		fmt.Printf("app %s is already on latest state (%s) \n", app.AppName, payload.RequestedState)
		return nil
	}

	if payload.CurrentState != "" {
		app.CurrentState = payload.CurrentState
	}

	if payload.RequestedState != "" {
		app.ManuallyRequestedState = payload.RequestedState
	}

	transitionFunc := sm.getTransitionFunc(app.CurrentState, payload.RequestedState)

	if transitionFunc == nil {
		fmt.Printf("Not yet implemented transition from %s to %s\n", app.CurrentState, payload.RequestedState)
		return nil
	}

	// ensure multiple transitions for the same app in parallel are not possible
	if app.IsTransitioning() {
		fmt.Printf("App with name %s and stage %s is already transitioning", app.AppName, app.Stage)
		return nil
	}

	errChannel := make(chan error)
	go func() {
		app.BeginTransition()
		err := transitionFunc(payload, app)

		// If anything goes wrong with the transition function
		// we should set the state change to FAILED
		// This will in turn update the in memory state and the local database state
		// which will in turn update the remote database as well
		if err != nil {
			setStateErr := sm.setState(app, common.FAILED)
			if setStateErr != nil {
				// wrap errors into one
				err = fmt.Errorf("Failed to complete transition: %w; Failed to set state to 'FAILED';", err)
			}
		}

		// send potential error to errChannel
		// if error = nil, the transition has completed successfully
		errChannel <- err

		// transition has finished
		app.FinishTransition()

		// Verify if app has the latest requested state
		// TODO: properly handle this error
		err = sm.VerifyState(app)
		if err != nil {
			fmt.Println("failed to verify state:", err)
		}
	}()

	go func() {
		err := <-errChannel
		close(errChannel)

		funcName := runtime.FuncForPC(reflect.ValueOf(transitionFunc).Pointer()).Name()
		if err == nil {
			fmt.Println("Successfully finished transaction function:", funcName)
			return
		}

		fmt.Printf("An error occured during transition from %s to %s using %s\n", app.CurrentState, payload.RequestedState, funcName)
		fmt.Println("The current app state will has been set to FAILED")
		fmt.Println()
		fmt.Println(err)
	}()

	return nil
}
