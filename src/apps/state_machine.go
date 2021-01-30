package apps

import (
	"context"
	"fmt"
	"reagent/api/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"
	"reflect"
	"runtime"

	"github.com/docker/docker/api/types"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver StateObserver
	LogManager    logging.LogManager
	Container     container.Container
	appStates     []common.App
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.pullAppOnDevice,
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
			common.PRESENT:     sm.pullAppOnDevice,
			common.RUNNING:     nil,
			common.BUILDING:    nil,
			common.PUBLISHING:  nil,
		},
		common.BUILDING: {
			common.PRESENT:     sm.stopBuildOnDevice,
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

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}
	app.CurrentState = state
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
		sm.appStates = append(sm.appStates, *app)

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

	err := transitionFunc(payload, app)

	// If anything goes wrong with the transition function
	// we should set the state change to FAILED
	// This will in turn update the in memory state and the local database state
	// which will in turn update the remote database as well
	// TODO: introduce concurrent error handling
	if err != nil {
		funcName := runtime.FuncForPC(reflect.ValueOf(transitionFunc).Pointer()).Name()
		fmt.Printf("An error occured during transition from %s to %s using %s\n", app.CurrentState, payload.RequestedState, funcName)
		fmt.Println(err)
		fmt.Println("The current app state will be set to FAILED")
		fmt.Println()

		extraErr := sm.setState(app, common.FAILED)
		if extraErr != nil {
			return extraErr
		}
		return err
	}

	return nil
}

func (sm *StateMachine) stopBuildOnDevice(payload common.TransitionPayload, app *common.App) error {
	id := sm.LogManager.GetActiveBuildId(payload.ContainerName)
	if id != "" {
		ctx := context.Background()
		err := sm.Container.CancelBuild(ctx, id)
		if err != nil {
			return err
		}
	}

	fmt.Println("No active build was found.")
	return nil
}

func (sm *StateMachine) buildAppOnDevice(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		ctx := context.Background() // TODO: store context in memory for build cancellation

		config := sm.Container.GetConfig()
		fileDir := config.CommandLineArguments.AppBuildsDirectory
		fileName := payload.AppName + config.CommandLineArguments.CompressedBuildExtension
		filePath := fileDir + "/" + fileName

		exists, _ := filesystem.FileExists(filePath)
		if !exists {
			sm.setState(app, common.FAILED)
			return fmt.Errorf("build files do not exist on path %s", filePath)
		}

		err := sm.setState(app, common.BUILDING)

		if err != nil {
			return err
		}

		reader, err := sm.Container.Build(ctx, filePath, types.ImageBuildOptions{Tags: []string{payload.RepositoryImageName}, Dockerfile: "Dockerfile"})

		if err != nil {
			return err
		}

		err = sm.LogManager.Stream(payload.ContainerName, logging.BUILD, reader)

		buildFailed := false
		if err != nil {
			if errdefs.IsBuildFailed(err) {
				buildFailed = true
			} else {
				return err
			}
		}

		app.ManuallyRequestedState = common.PRESENT
		err = sm.setState(app, common.PRESENT)
		if err != nil {
			return err
		}

		buildResultMessage := "Image built successfully"
		if buildFailed {
			buildResultMessage = "Image build failed to complete"
		}

		err = sm.LogManager.Write(payload.ContainerName, logging.BUILD, fmt.Sprintf("%s", buildResultMessage))
		if err != nil {
			return err
		}
	}

	return nil
}

func (sm *StateMachine) pullAppOnDevice(payload common.TransitionPayload, app *common.App) error {
	config := sm.Container.GetConfig()
	if payload.Stage == common.DEV {
		return fmt.Errorf("a dev stage app is not available on the registry")
	}

	err := sm.setState(app, common.DOWNLOADING)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Need to authenticate to private registry to determine proper privileges to pull the app
	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	reader, err := sm.Container.Pull(ctx, payload.RepositoryImageName, authConfig)
	if err != nil {
		return err
	}
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName, logging.PULL, reader)
	if err != nil {
		return err
	}

	return nil
}
