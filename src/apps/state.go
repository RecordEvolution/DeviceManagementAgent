package apps

import (
	"context"
	"fmt"
	"reagent/container"
	"reagent/logging"

	"github.com/docker/docker/api/types"
)

// AppState states
type AppState string

const (
	PRESENT      AppState = "PRESENT"
	REMOVED      AppState = "REMOVED"
	UNINSTALLED  AppState = "UNINSTALLED"
	FAILED       AppState = "FAILED"
	BUILDING     AppState = "BUILDING"
	TRANSFERRED  AppState = "TRANSFERRED"
	TRANSFERRING AppState = "TRANSFERRING"
	PUBLISHING   AppState = "PUBLISHING"
	DOWNLOADING  AppState = "DOWNLOADING"
	STARTING     AppState = "STARTING"
	STOPPING     AppState = "STOPPING"
	UPDATING     AppState = "UPDATING"
	DELETING     AppState = "DELETING"
	RUNNING      AppState = "RUNNING"
)

type Stage string

const (
	DEV  Stage = "DEV"
	PROD Stage = "PROD"
)

type TransitionFunc func(TransitionPayload) error

type StateMachine struct {
	currentAppStates []App
	container        container.Container
	observer         StateObserver
	log              logging.LogManager
}

type TransitionPayload struct {
	stage         Stage
	appName       string
	appKey        int
	imageName     string
	containerName string
	accountID     int
	registryToken string
}

type App struct {
	Name                   string `json:"name"`
	AppKey                 int    `json:"app_key"`
	AppName                string `json:"app_name"`
	ManuallyRequestedState string `json:"manually_requested_state"`
	CurrentState           AppState
	Stage                  Stage
}

func (sm *StateMachine) getTransitionFunc(prevState AppState, nextState AppState) TransitionFunc {
	var stateTransitionMap = map[AppState]map[AppState]TransitionFunc{
		REMOVED: {
			PRESENT:     nil,
			RUNNING:     nil,
			BUILDING:    sm.buildAppOnDevice,
			PUBLISHING:  nil,
			UNINSTALLED: nil,
		},
		UNINSTALLED: {
			PRESENT:    nil,
			RUNNING:    nil,
			BUILDING:   nil,
			PUBLISHING: nil,
		},
		PRESENT: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
		},
		FAILED: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
			RUNNING:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
		},
		BUILDING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			PUBLISHING:  nil,
		},
		TRANSFERRED: {
			BUILDING:    nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
		},
		TRANSFERRING: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
		},
		PUBLISHING: {
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		RUNNING: {
			PRESENT:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		DOWNLOADING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		STARTING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		STOPPING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		UPDATING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		DELETING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
	}

	return stateTransitionMap[prevState][nextState]
}

func (sm *StateMachine) getCurrentState(appName string, stage Stage) *AppState {
	for _, state := range sm.currentAppStates {
		if state.AppName == appName && state.Stage == stage {
			return &state.CurrentState
		}
	}
	return nil
}

func (sm *StateMachine) setState(app *App, state AppState) {
	app.CurrentState = state
	sm.observer.Notify(app, state)
}

func (sm *StateMachine) getApp(appName string, appKey int, stage Stage) (*App, error) {
	for _, state := range sm.currentAppStates {
		if state.AppName == appName && state.Stage == stage {
			return &state, nil
		}
	}
	return nil, fmt.Errorf("App was not found")
}

func (sm *StateMachine) RequestAppState(requestedState AppState, payload TransitionPayload) {
	currentState := sm.getCurrentState(payload.appName, payload.stage)
	transitionFunc := sm.getTransitionFunc(*currentState, requestedState)
	transitionFunc(payload)
}

func (sm *StateMachine) buildAppOnDevice(payload TransitionPayload) error {
	app, err := sm.getApp(payload.appName, payload.appKey, payload.stage)

	if err != nil {
		return err
	}

	if payload.stage == DEV {
		ctx := context.Background() // TODO: store context in memory for build cancellation
		reader, err := sm.container.Build(ctx, "./TestApp.tar", types.ImageBuildOptions{Tags: []string{payload.imageName}, Dockerfile: "Dockerfile"})
		sm.log.Broadcast(payload.containerName, logging.BUILD, reader)

		if err != nil {
			return err
		}

		if err != nil {
			sm.setState(app, FAILED)
		}
		sm.setState(app, PRESENT)
	}

	return nil
}
