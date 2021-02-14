package apps

import (
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
	"reflect"
	"runtime"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver *StateObserver
	StateUpdater  *StateUpdater
	Container     container.Container
	LogManager    *logging.LogManager
	appStates     []*common.App
}

func NewStateMachine(container container.Container, logManager *logging.LogManager, observer *StateObserver, updater *StateUpdater) StateMachine {
	appStates := make([]*common.App, 0)
	return StateMachine{
		StateObserver: observer,
		StateUpdater:  updater,
		Container:     container,
		LogManager:    logManager,
		appStates:     appStates,
	}
}

var cancelableTransitions = []common.AppState{common.BUILDING, common.PUBLISHING, common.DOWNLOADING}

func (sm *StateMachine) isCancelable(app *common.App) bool {
	for _, transition := range cancelableTransitions {
		if app.CurrentState == transition {
			return true
		}
	}
	return false
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     sm.pullAndRunApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.UNINSTALLED: {
			common.PRESENT:   sm.pullApp,
			common.RUNNING:   sm.runApp,
			common.BUILT:     sm.buildApp,
			common.PUBLISHED: sm.publishApp,
		},
		common.BUILDING: {
			common.REMOVED: sm.cancelBuild,
		},
		common.PRESENT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.FAILED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
			common.PRESENT:     sm.recoverFailToPresentHandler,
			common.RUNNING:     nil,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   nil,
		},
		common.BUILT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     nil,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.TRANSFERED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.pullApp,
		},
		common.TRANSFERING: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.pullApp,
		},
		common.PUBLISHED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.PRESENT:     nil,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.RUNNING: {
			common.PRESENT:     sm.stopApp,
			common.BUILT:       sm.stopApp,
			common.PUBLISHED:   sm.removeAndPublishApp,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.DOWNLOADING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.STARTING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.STOPPING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.UPDATING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.DELETING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
	}

	return stateTransitionMap[prevState][nextState]
}

func (sm *StateMachine) setLocalState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.NotifyLocalOnly(app, state)
	if err != nil {
		return err
	}
	app.CurrentState = state
	return nil
}

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}
	app.CurrentState = state
	return nil
}

func (sm *StateMachine) findApp(appKey uint64, stage common.Stage) *common.App {
	for i := range sm.appStates {
		state := sm.appStates[i]
		if state.AppKey == appKey && state.Stage == stage {
			return state
		}
	}
	return nil
}

func (sm *StateMachine) updateCurrentAppState(payload common.TransitionPayload, app *common.App) error {
	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if app.CurrentState == common.BUILT || app.CurrentState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	return sm.setLocalState(app, app.CurrentState)
}

// GetOrInitAppState gets the state of an app that is currently in memory. If an app state does not exist with given key and stage, it will create a new entry. This entry will be stored in memory and in the local database
//
// The state machine is not responsible for fetching state from the local database and will only concern itself with the app states that has been preloaded.
func (sm *StateMachine) GetOrInitAppState(payload common.TransitionPayload) (*common.App, error) {
	app := sm.findApp(payload.AppKey, payload.Stage)

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app = &common.App{
			AppKey:              payload.AppKey,
			AppName:             payload.AppName,
			CurrentState:        payload.CurrentState,
			DeviceToAppKey:      payload.DeviceToAppKey,
			ReleaseKey:          payload.ReleaseKey,
			RequestorAccountKey: payload.RequestorAccountKey,
			RequestedState:      payload.RequestedState,
			Stage:               payload.Stage,
			Version:             payload.PresentVersion,
			RequestUpdate:       payload.RequestUpdate,
			Semaphore:           semaphore.NewWeighted(1),
		}
		// It is possible that there is already a current app state
		// if we receive a sync request from the remote database
		// in that case, take that one
		if payload.CurrentState == "" {
			// Set the state of the newly added app to REMOVED
			app.CurrentState = common.REMOVED
		}

		// If app does not exist in database, it will be added
		// FIXME: no need to set local database state since we already have the most up to date local state (we update from remote before this ever gets called)
		// instead we should just populate the state machine with the local data, or have the state machine directly read from the database
		err := sm.setLocalState(app, app.CurrentState)
		if err != nil {
			return nil, err
		}

		sm.appStates = append(sm.appStates, app)

	}

	// always update the local app's requestedState using the transition payload
	if payload.RequestedState != "" {
		app.RequestedState = payload.RequestedState
	}

	return app, nil
}

func (sm *StateMachine) executeTransition(app *common.App, payload common.TransitionPayload, transitionFunc TransitionFunc, skipUnlock bool) {
	errChannel := make(chan error)

	go func() {
		log.Info().Msgf("Executing transition from %s to %s for %s (%s)...", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
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
	}()

	go func() {
		err := <-errChannel
		close(errChannel)

		// if we weren't locked in this transition we can skip unlocking
		if !skipUnlock {
			// allow next state transition
			app.UnlockTransition()
		}

		funcName := runtime.FuncForPC(reflect.ValueOf(transitionFunc).Pointer()).Name()
		if err == nil {
			log.Info().Msgf("Successfully finished transaction function:", funcName)

			// Verify if app has the latest requested state
			// TODO: properly handle it when verifying fails
			sm.StateUpdater.VerifyState(app, sm.RequestAppState)
			return
		}

		log.Error().Msgf("An error occured during transition from %s to %s using %s", app.CurrentState, payload.RequestedState, funcName)
		log.Error().Err(err).Msg("The current app state will has been set to FAILED")
	}()
}

func (sm *StateMachine) RequestAppState(payload common.TransitionPayload) error {
	app, err := sm.GetOrInitAppState(payload)
	if err != nil {
		return err
	}

	// allow canceling state transitions to perform a transition
	if sm.isCancelable(app) {
		// only allow cancelation with certain states
		if payload.RequestedState == common.REMOVED && app.CurrentState == common.BUILDING {
			transitionFunc := sm.getTransitionFunc(app.CurrentState, payload.RequestedState)
			sm.executeTransition(app, payload, transitionFunc, true)
			return nil
		}
	}

	// prevent concurrent state transitions for same app
	if app.IsTransitioning() {
		log.Warn().Msgf("App with name %s and stage %s is already transitioning", app.AppName, app.Stage)
		return nil
	}

	// Update the app data with the provided payload
	err = sm.updateCurrentAppState(payload, app)
	if err != nil {
		app.UnlockTransition()
		return err
	}

	// If appState is already up to date we should do nothing
	// It's possible to go from a built/published state to a built/published state since both represent a present state
	if app.CurrentState == payload.RequestedState && !payload.RequestUpdate && app.CurrentState != common.BUILT && app.CurrentState != common.PUBLISHED {
		log.Debug().Msgf("app %s (%s) is already on latest state (%s)", app.AppName, app.Stage, payload.RequestedState)
		app.UnlockTransition()
		return nil
	}

	var transitionFunc TransitionFunc
	if payload.RequestUpdate && payload.NewestVersion != app.Version {
		transitionFunc = sm.getUpdateTransition(payload, app)
	} else {
		transitionFunc = sm.getTransitionFunc(app.CurrentState, payload.RequestedState)
	}

	if transitionFunc == nil {
		log.Debug().Msgf("Not yet implemented transition from %s to %s", app.CurrentState, payload.RequestedState)
		app.UnlockTransition()
		return nil
	}

	sm.executeTransition(app, payload, transitionFunc, false)
	return nil
}
