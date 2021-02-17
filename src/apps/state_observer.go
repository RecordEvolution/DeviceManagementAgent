package apps

import (
	"context"
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
		log.Debug().Msgf("State Observer: removed an observer for %s (%s)", appName, stage)
	}
	so.mapMutex.Unlock()
}

func (so *StateObserver) addObserver(stage common.Stage, appKey uint64, appName string) {
	containerName := common.BuildContainerName(stage, appKey, appName)

	so.mapMutex.Lock()
	if so.activeObservers[containerName] == nil {
		errC := so.observeAppState(stage, appKey, appName)
		log.Debug().Msgf("State Observer: created an observer for %s (%s)", appName, stage)
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

	rStates, err := so.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	for _, rState := range rStates {

		containerName := common.BuildContainerName(rState.Stage, rState.AppKey, rState.AppName)
		app, err := so.AppStore.GetApp(rState.AppKey, rState.Stage)
		if err != nil {
			return err
		}

		container, err := so.Container.GetContainerState(ctx, containerName)
		if err != nil {
			if errdefs.IsContainerNotFound(err) {

				// should set the app state to 'removed' if it wasn't already
				if app.CurrentState != common.REMOVED && app.CurrentState != common.UNINSTALLED {
					log.Debug().Msgf("State Correcter: irregular state was found for app %s (%s) that has no container on the device", rState.AppName, rState.Stage)
					log.Debug().Msgf("State Correcter: app state for %s will be updated to %s", containerName, common.REMOVED)

					err := so.Notify(app, common.REMOVED)
					if err != nil {
						return err
					}
				}

				// should be all good iterate over next app
				continue

			} else {
				return err
			}
		}

		appStateDeterminedByContainer, err := common.ContainerStateToAppState(container.Status, int(container.ExitCode))
		if err != nil {
			return err
		}

		correctedAppState := app.CurrentState

		switch app.CurrentState {
		case common.DOWNLOADING,
			common.TRANSFERING,
			common.BUILDING:
			correctedAppState = common.REMOVED
		case common.PUBLISHING,
			common.STOPPING,
			common.STARTING:
			correctedAppState = appStateDeterminedByContainer
		default:
			correctedAppState = appStateDeterminedByContainer
		}

		if correctedAppState == app.CurrentState {
			log.Debug().Msgf("State Correcter: app state for %s is currently: %s and correct, nothing to do.", containerName, app.CurrentState)
			return nil
		}

		log.Debug().Msgf("State Correcter: app state for %s will be updated to %s", containerName, correctedAppState)
		err = so.Notify(app, correctedAppState)
		if err != nil {
			return err
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

				log.Debug().Msgf("State Observer: No container was found for %s, removing observer..", containerName)
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
			latestAppState, err := common.ContainerStateToAppState(state.Status, state.ExitCode)
			if err != nil {
				errorC <- err
				return
			}

			app, err := so.AppStore.GetApp(appKey, stage)
			if err != nil {
				errorC <- err
				return
			}

			if app.CurrentState != latestAppState && !common.IsTransientState(app.CurrentState) && !common.IsTransientState(latestAppState) {
				log.Debug().Msgf("State Observer: app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("State Observer: app (%s, %s) updating from %s to %s", appName, stage, app.CurrentState, latestAppState)

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

	_ = so.initObserverSpawner()

	return err
}
