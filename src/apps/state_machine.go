package apps

import (
	"context"
	"fmt"
	"reagent/api/common"
	"reagent/container"
	"reagent/logging"

	"github.com/docker/docker/api/types"
)

type TransitionFunc func(transitionPayload TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver StateObserver
	LogManager    logging.LogManager
	Container     container.Container
	appStates     []common.App
}

// TransitionPayload provides the data used by the StateMachine to transition between states.
type TransitionPayload struct {
	RequestedState      common.AppState
	CurrentState        common.AppState
	Stage               common.Stage
	AppName             string
	AppKey              uint64
	ImageName           string
	RepositoryImageName string
	ContainerName       string
	AccountID           string
	RegisteryToken      string
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     nil,
			common.RUNNING:     nil,
			common.BUILDING:    sm.buildAppOnDevice,
			common.PUBLISHING:  nil,
			common.UNINSTALLED: nil,
		},
		common.UNINSTALLED: {
			common.PRESENT:    nil,
			common.RUNNING:    nil,
			common.BUILDING:   nil,
			common.PUBLISHING: nil,
		},
		common.PRESENT: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.RUNNING:     nil,
			common.BUILDING:    nil,
			common.PUBLISHING:  nil,
		},
		common.FAILED: {
			common.REMOVED:     nil,
			common.UNINSTALLED: nil,
			common.PRESENT:     nil,
			common.RUNNING:     nil,
			common.BUILDING:    nil,
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
			common.PRESENT:     nil,
			common.BUILDING:    nil,
			common.PUBLISHING:  nil,
			common.REMOVED:     nil,
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

func (sm *StateMachine) PopulateState(apps []common.App) {
	sm.appStates = apps
}

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	app.CurrentState = state
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) getApp(appKey uint64, stage common.Stage) *common.App {
	for _, state := range sm.appStates {
		if state.AppKey == appKey && state.Stage == stage {
			return &state
		}
	}
	return nil
}

func (sm *StateMachine) RequestAppState(payload TransitionPayload) error {
	app := sm.getApp(payload.AppKey, payload.Stage)

	// If appState is already up to date we should do nothing
	if app != nil && app.CurrentState == payload.RequestedState {
		return nil
	}

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app = &common.App{
			Name:                   payload.AppName,
			AppKey:                 payload.AppKey,
			AppName:                payload.AppName,
			CurrentState:           payload.CurrentState,
			ManuallyRequestedState: payload.RequestedState,
			Stage:                  payload.Stage,
			RequestUpdate:          false,
		}
		sm.appStates = append(sm.appStates, *app)

		// It is possible that there is already a current app state
		// if we receive a sync request from the remote database
		// in that case, take that one
		currentState := app.CurrentState
		if currentState == "" {
			// Set the state of the newly added app to REMOVED
			app.CurrentState = common.REMOVED
		}

		// TODO: since the remote database state is already set whenever we received a currentState, we do not need to update the remote app state again

		// If app does not exist in database, it will be added
		// + remote app state will be updated
		sm.setState(app, app.CurrentState)
	}

	transitionFunc := sm.getTransitionFunc(app.CurrentState, payload.RequestedState)

	if transitionFunc == nil {
		fmt.Printf("Not yet implemented transition from %s to %s", app.CurrentState, payload.RequestedState)
		return fmt.Errorf("Not yet implemented transition from %s to %s", app.CurrentState, payload.RequestedState)
	}

	err := transitionFunc(payload, app)

	// If anything goes wrong with the transition function
	// we should set the state change to FAILED
	// This will in turn update the in memory state and the local database state
	// which will in turn update the remote database as well
	if err != nil {
		extraErr := sm.setState(app, common.FAILED)
		if extraErr != nil {
			return extraErr
		}
		return err
	}

	return nil
}

func (sm *StateMachine) buildAppOnDevice(payload TransitionPayload, app *common.App) error {
	err := sm.setState(app, common.BUILDING)

	if err != nil {
		return err
	}

	if payload.Stage == common.DEV {
		ctx := context.Background() // TODO: store context in memory for build cancellation

		reader, err := sm.Container.Build(ctx, "./TestApp.tar", types.ImageBuildOptions{Tags: []string{payload.RepositoryImageName}, Dockerfile: "Dockerfile"})

		if err != nil {
			return err
		}

		err = sm.LogManager.Stream(payload.ContainerName, logging.BUILD, reader)
		if err != nil {
			return err
		}

		sm.setState(app, common.PRESENT)
		sm.LogManager.Write(payload.ContainerName, logging.BUILD, "#################### Image built successfully ####################")
	}

	return nil
}
