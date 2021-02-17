package apps

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/store"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type AppStateObserver struct {
	Stage   common.Stage
	AppKey  uint64
	AppName string
	errChan chan error
}

type StateObserver struct {
	AppStore        *store.AppStore
	Container       container.Container
	activeObservers map[string]*AppStateObserver
	mapMutex        sync.Mutex
}

func NewObserver(container container.Container, appStore *store.AppStore) StateObserver {
	return StateObserver{
		Container:       container,
		AppStore:        appStore,
		activeObservers: make(map[string]*AppStateObserver),
	}
}

// Notify is used by the StateMachine to notify the observer that the app state has changed
func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {

	// update in memory
	app.CurrentState = achievedState

	// update remotely
	err := so.AppStore.UpdateRemoteAppState(app, achievedState)
	if err != nil {
		log.Error().Stack().Err(err)
		// ignore
	}

	go func() {
		// update locally
		_, err = so.AppStore.UpdateLocalAppState(app, achievedState)
		if err != nil {
			log.Error().Stack().Err(err)
			return
		}
	}()

	return nil
}

func (so *StateObserver) removeObserver(stage common.Stage, appKey uint64, appName string) {
	containerName := common.BuildContainerName(stage, appKey, appName)
	so.mapMutex.Lock()
	observer := so.activeObservers[containerName]

	if observer != nil {
		close(observer.errChan)
		delete(so.activeObservers, containerName)
		log.Debug().Msgf("removed an observer for %s (%s)", appName, stage)
	}
	so.mapMutex.Unlock()
}

func (so *StateObserver) addObserver(stage common.Stage, appKey uint64, appName string) {
	containerName := common.BuildContainerName(stage, appKey, appName)

	so.mapMutex.Lock()
	if so.activeObservers[containerName] == nil {
		errC := so.observeAppState(stage, appKey, appName)
		so.activeObservers[containerName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
		}
	}
	so.mapMutex.Unlock()
}

// CorrectLocalAndUpdateRemoteAppStates ensures that the app state corresponds with the container status of the app.
// Any transient states will be handled accordingly. After the states have been assured, it will attempt to update the app states remotely.
func (so *StateObserver) CorrectLocalAndUpdateRemoteAppStates() error {
	ctx := context.Background()
	containers, err := so.Container.ListContainers(ctx, nil)
	if err != nil {
		return err
	}

	for _, container := range containers {
		// Can be one of "created", "running", "paused", "restarting", "removing", "exited", or "dead"
		for _, containerName := range container.Names {
			stage, appKey, _, err := common.ParseContainerName(containerName)
			if err != nil {
				continue
			}

			app, err := so.AppStore.GetApp(appKey, stage)

			if app == nil {
				log.Debug().Msgf("found a container (%s) that does not have a corresponding app, skipping", containerName)
				return nil
			}

			if err != nil {
				return err
			}

			stateAppShouldBe, err := containerStatusToAppState(container.Status, int(container.ExitCode))

			switch app.CurrentState {
			case common.DOWNLOADING:
			case common.TRANSFERING:
			case common.BUILDING:
				app.CurrentState = common.REMOVED
				break
			case common.PUBLISHING:
			case common.STOPPING:
			case common.STARTING:
				app.CurrentState = stateAppShouldBe
				break
			default:
				if app.CurrentState != stateAppShouldBe {
					app.CurrentState = stateAppShouldBe
				}
			}

			err = so.Notify(app, stateAppShouldBe)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

func (so *StateObserver) observeAppState(stage common.Stage, appKey uint64, appName string) chan error {
	ctx := context.Background()
	containerName := common.BuildContainerName(stage, appKey, appName)
	errorC := make(chan error, 1)
	pollingRate := time.Second * 2

	go func() {
		lastKnownStatus := "UKNOWN"

		defer so.removeObserver(stage, appKey, appName)

		for {
			state, err := so.Container.GetContainerState(ctx, containerName)
			if err != nil {
				if !errdefs.IsContainerNotFound(err) {
					errorC <- err
					return
				}

				// don't return error, we just finish
				return
			}

			// status change detected
			// always executed the state check on init
			if lastKnownStatus == state.Status {
				// log.Debug().Msgf("app (%s, %s) container status remains unchanged: %s", stage, appName, lastKnownStatus)
				time.Sleep(pollingRate)
				continue
			}

			// check if correct and update database if needed
			latestAppState, err := containerStatusToAppState(state.Status, state.ExitCode)
			if err != nil {
				errorC <- err
				return
			}

			app, err := so.AppStore.GetApp(appKey, stage)
			if err != nil {
				errorC <- err
				return
			}

			if app.CurrentState != latestAppState && !isTransientState(app.CurrentState) && !isTransientState(latestAppState) {
				log.Debug().Msgf("Observer: app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("Observer: app (%s, %s) updating from %s to %s", appName, stage, app.CurrentState, latestAppState)

				// update the app state
				err := so.Notify(app, latestAppState)
				if err != nil {
					errorC <- err
					return
				}
			}

			// update the last recorded status
			lastKnownStatus = state.Status

			time.Sleep(pollingRate)
		}
	}()

	return errorC
}

func (so *StateObserver) initObserverSpawner() chan error {
	messageC, errC := so.Container.ListenForContainerEvents(context.Background())

	errChan := make(chan error, 1)

	go func() {
	loop:
		for {
			select {
			case event := <-messageC:
				switch event.Action {
				case "create":
					containerName := event.Actor.Attributes["name"]
					stage, key, name, err := common.ParseContainerName(containerName)
					if err != nil {
						errChan <- err
						close(errChan)
						break loop
					}
					log.Debug().Msgf("added an observer for %s (%s)", name, stage)
					so.addObserver(stage, key, name)
				}
			case err := <-errC:
				errChan <- err
				close(errChan)
				break loop
			}
		}
	}()

	return errChan
}

func (so *StateObserver) ObserveAppStates() error {
	// all the apps currently available
	apps, err := so.AppStore.GetAllApps()
	if err != nil {
		return err
	}

	for _, app := range apps {
		so.addObserver(app.Stage, app.AppKey, app.AppName)
	}

	// TODO: handle potential errors
	_ = so.initObserverSpawner()

	return err
}

func isTransientState(appState common.AppState) bool {
	switch appState {
	case common.BUILDING,
		common.BUILT,
		common.TRANSFERED,
		common.TRANSFERING,
		common.PUBLISHING,
		common.PUBLISHED,
		common.DOWNLOADING,
		common.UPDATING,
		common.DELETING,
		common.STARTING:
		return true
	}
	return false
}

func containerStatusToAppState(containerStatus string, exitCode int) (common.AppState, error) {
	unknownStateErr := errors.New("unkown state")

	switch containerStatus {
	case "running":
		return common.RUNNING, nil
	case "created":
		return "", unknownStateErr
	case "removing":
		return common.STOPPING, nil
	case "paused": // won't occur (as of writing)
		return "", unknownStateErr
	case "restarting":
		return common.FAILED, nil
	case "exited":
		if exitCode == 0 {
			return common.PRESENT, nil
		}
		return common.FAILED, nil
	case "dead":
		return common.FAILED, nil
	}

	return "", unknownStateErr
}
