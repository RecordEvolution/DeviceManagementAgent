package apps

import (
	"context"
	"fmt"
	"reagent/api/common"
	"reagent/container"
	"reagent/logging"

	"github.com/docker/docker/api/types"
)

type TransitionFunc func(TransitionPayload) error

type StateMachine struct {
	StateObserver StateObserver
	LogManager    logging.LogManager
	Container     container.Container
	appStates     []common.App
}

type TransitionPayload struct {
	RequestedState      common.AppState
	Stage               common.Stage
	AppName             string
	AppKey              uint64
	ImageName           string
	FullImageName       string
	RepositoryImageName string
	ContainerName       string
	AccountID           int
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

func (sm *StateMachine) getCurrentState(appName string, stage common.Stage) (*common.AppState, error) {
	fmt.Println(sm.appStates)
	for _, state := range sm.appStates {
		if state.AppName == appName && state.Stage == stage {
			return &state.CurrentState, nil
		}
	}
	return nil, fmt.Errorf("Could not locate app state in memory")
}

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}

	app.CurrentState = state
	return nil
}

func (sm *StateMachine) getApp(appName string, appKey uint64, stage common.Stage) (*common.App, error) {
	for _, state := range sm.appStates {
		if state.AppName == appName && state.Stage == stage {
			return &state, nil
		}
	}
	return nil, fmt.Errorf("App was not found")
}

func (sm *StateMachine) AddAppState(app common.App) {
	sm.appStates = append(sm.appStates, app)
}

func (sm *StateMachine) PopulateAppStates(apps []common.App) {
	sm.appStates = apps
}

func (sm *StateMachine) RequestAppState(payload TransitionPayload) error {
	currentState, err := sm.getCurrentState(payload.AppName, payload.Stage)
	if err != nil {
		return err
	}

	transitionFunc := sm.getTransitionFunc(*currentState, payload.RequestedState)
	return transitionFunc(payload)
}

func (sm *StateMachine) buildAppOnDevice(payload TransitionPayload) error {
	app, err := sm.getApp(payload.AppName, payload.AppKey, payload.Stage)

	if err != nil {
		return err
	}

	err = sm.setState(app, common.BUILDING)

	if err != nil {
		return err
	}

	if payload.Stage == common.DEV {
		ctx := context.Background() // TODO: store context in memory for build cancellation

		reader, err := sm.Container.Build(ctx, "./TestApp.tar", types.ImageBuildOptions{Tags: []string{payload.FullImageName}, Dockerfile: "Dockerfile"})

		if err != nil {
			return err
		}

		err = sm.LogManager.Stream(payload.ContainerName, logging.BUILD, reader)
		if err != nil {
			sm.setState(app, common.FAILED)
		} else {
			sm.setState(app, common.PRESENT)
			sm.LogManager.Write(payload.ContainerName, logging.BUILD, "#################### Image built successfully ####################")
		}
	}

	return nil
}
