package apps

import (
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
	"reflect"
	"runtime"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App, errorChannel chan error)

type StateMachine struct {
	StateObserver StateObserver
	LogManager    logging.LogManager
	Container     container.Container
	appStates     []*common.App
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     nil,
			common.BUILDING:    sm.buildApp,
			common.PUBLISHING:  nil,
			common.UNINSTALLED: nil,
		},
		common.UNINSTALLED: {
			common.PRESENT:    sm.pullApp,
			common.RUNNING:    nil,
			common.BUILDING:   nil,
			common.PUBLISHING: nil,
		},
		common.PRESENT: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     sm.runApp,
			common.BUILDING:    sm.buildApp,
			common.PUBLISHING:  nil,
		},
		common.FAILED: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     nil,
			common.BUILDING:    sm.buildApp,
			common.PUBLISHING:  nil,
		},
		common.BUILDING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PUBLISHING:  nil,
		},
		common.TRANSFERRED: {
			common.BUILDING:    nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     nil,
		},
		common.TRANSFERRING: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     nil,
		},
		common.PUBLISHING: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
		},
		common.RUNNING: {
			common.PRESENT:     sm.stopApp,
			common.BUILDING:    nil,
			common.PUBLISHING:  nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
		},
		common.DOWNLOADING: {
			common.PRESENT:     sm.pullApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
		},
		common.STARTING: {
			common.PRESENT:     sm.pullApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.STOPPING: {
			common.PRESENT:     sm.pullApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.UPDATING: {
			common.PRESENT:     sm.pullApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
		},
		common.DELETING: {
			common.PRESENT:     sm.pullApp,
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
		sm.appStates = append(sm.appStates, app)

		// It is possible that there is already a current app state
		// if we receive a sync request from the remote database
		// in that case, take that one
		if payload.CurrentState == "" {
			// Set the state of the newly added app to REMOVED
			app.CurrentState = common.REMOVED
		}

		// If app does not exist in database, it will be added
		// + remote app state will be updated
		// TODO: since the remote database state is already set whenever we received a currentState, we do not need to update the remote app state again
		sm.setState(app, app.CurrentState)
	}

	// If appState is already up to date we should do nothing
	if app.CurrentState == payload.RequestedState {
		fmt.Printf("app %s is already on latest state (%s) \n", app.AppName, payload.RequestedState)
		return nil
	}

	transitionFunc := sm.getTransitionFunc(app.CurrentState, payload.RequestedState)

	if transitionFunc == nil {
		fmt.Printf("Not yet implemented transition from %s to %s\n", app.CurrentState, payload.RequestedState)
		return nil
	}

	errChannel := make(chan error)
	go transitionFunc(payload, app, errChannel)

	go func() {
		err := <-errChannel
		close(errChannel)

		funcName := runtime.FuncForPC(reflect.ValueOf(transitionFunc).Pointer()).Name()
		if err == nil {
			fmt.Println("Successfully finished transaction function:", funcName)
			return
		}

		fmt.Printf("An error occured during transition from %s to %s using %s\n", app.CurrentState, payload.RequestedState, funcName)
		fmt.Println(err)
		fmt.Println()
		fmt.Println("The current app state will be set to FAILED")

		// If anything goes wrong with the transition function
		// we should set the state change to FAILED
		// This will in turn update the in memory state and the local database state
		// which will in turn update the remote database as well
		err = sm.setState(app, common.FAILED)
		if err != nil {
			fmt.Println("Failed to set local app state to 'FAILED'", err)
		}
	}()

	return nil
}
