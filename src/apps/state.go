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
	Container        container.Container
	StateObserver    StateObserver
	LogManager       logging.LogManager
}

type TransitionPayload struct {
	Stage               Stage
	AppName             string
	AppKey              uint64
	ImageName           string
	FullImageName       string
	RepositoryImageName string
	ContainerName       string
	AccountID           int
	RegisteryToken      string
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
	fmt.Println(sm.currentAppStates)
	for _, state := range sm.currentAppStates {
		if state.AppName == appName && state.Stage == stage {
			return &state.CurrentState
		}
	}
	return nil
}

func (sm *StateMachine) setState(app *App, state AppState) {
	sm.StateObserver.Notify(app, state)
	app.CurrentState = state
}

func (sm *StateMachine) getApp(appName string, appKey uint64, stage Stage) (*App, error) {
	for _, state := range sm.currentAppStates {
		if state.AppName == appName && state.Stage == stage {
			return &state, nil
		}
	}
	return nil, fmt.Errorf("App was not found")
}

func (sm *StateMachine) AddAppState(app App) {
	sm.currentAppStates = append(sm.currentAppStates, app)
}

func (sm *StateMachine) RequestAppState(payload TransitionPayload, requestedState AppState) error {
	currentState := sm.getCurrentState(payload.AppName, payload.Stage)
	transitionFunc := sm.getTransitionFunc(*currentState, requestedState)
	return transitionFunc(payload)
}

func (sm *StateMachine) buildAppOnDevice(payload TransitionPayload) error {
	app, err := sm.getApp(payload.AppName, payload.AppKey, payload.Stage)

	if err != nil {
		return err
	}

	if payload.Stage == DEV {
		ctx := context.Background() // TODO: store context in memory for build cancellation

		reader, err := sm.Container.Build(ctx, "./TestApp.tar", types.ImageBuildOptions{Tags: []string{}, Dockerfile: "Dockerfile"})
		if err != nil {
			return err
		}
		sm.LogManager.Broadcast(payload.ContainerName, logging.BUILD, reader)
		if err != nil {
			sm.setState(app, FAILED)
		} else {
			sm.setState(app, PRESENT)
		}
	}

	return nil
}
