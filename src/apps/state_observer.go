package apps

import (
	"context"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/safe"
	"reagent/store"
	"regexp"
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
	AppManager      *AppManager
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
	app.StateLock.Lock()
	app.CurrentState = achievedState
	app.StateLock.Unlock()

	// update remotely
	err := so.AppStore.UpdateRemoteAppState(app, achievedState)
	if err != nil {
		log.Error().Stack().Err(err)
		// ignore
	}

	safe.Go(func() {
		// update locally
		_, err = so.AppStore.UpdateLocalAppState(app, achievedState)
		if err != nil {
			log.Error().Stack().Err(err)
			return
		}
	})

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

		ctx := context.Background()
		container, err := so.Container.GetContainerState(ctx, containerName)
		if err != nil {
			if errdefs.IsContainerNotFound(err) {
				// we should check if the image exists, if it does not, we should set the state to 'REMOVED', else to 'STOPPED'

				var fullImageName string
				if rState.Stage == common.DEV {
					fullImageName = rState.RegistryImageName.Dev
				} else if rState.Stage == common.PROD {
					fullImageName = rState.RegistryImageName.Prod
				}

				images, err := so.Container.GetImages(ctx, fullImageName)
				if err != nil {
					return err
				}

				app.StateLock.Lock()

				correctedStage := app.CurrentState
				if len(images) == 0 {
					// no images was found (and no container) for this guy, so this guy is REMOVED
					if app.CurrentState != common.REMOVED && app.CurrentState != common.UNINSTALLED {
						correctedStage = common.REMOVED
					}
				} else {
					// images were found for this guy, but no container, this means --> PRESENT
					if app.CurrentState != common.PRESENT {
						correctedStage = common.PRESENT
					}
				}
				app.StateLock.Unlock()

				// notify the remote database of any changed states due to correction
				err = so.Notify(app, correctedStage)
				if err != nil {
					return err
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

		app.StateLock.Lock()
		correctedAppState := app.CurrentState
		currentAppState := app.CurrentState
		app.StateLock.Unlock()

		switch currentAppState {
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

		if correctedAppState == currentAppState && rState.CurrentState == correctedAppState {
			log.Debug().Msgf("State Correcter: app state for %s is currently: %s and correct, nothing to do.", containerName, app.CurrentState)
			continue
		}

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
	pollingRate := time.Second * 1

	safe.Go(func() {
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

				log.Error().Err(err).Msg("failed to parse latestAppState")
				return
			}

			app, err := so.AppStore.GetApp(appKey, stage)
			if err != nil {
				errorC <- err
				log.Error().Err(err).Msg("failed to get app")

				return
			}

			app.StateLock.Lock()
			curAppState := app.CurrentState
			app.StateLock.Unlock()

			if curAppState != latestAppState && !common.IsTransientState(curAppState) && !common.IsTransientState(latestAppState) {
				log.Debug().Msgf("State Observer: app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("State Observer: app (%s, %s) updating from %s to %s", appName, stage, curAppState, latestAppState)

				// update the current local and remote app state
				err := so.Notify(app, latestAppState)
				if err != nil {
					errorC <- err
					log.Error().Err(err).Msg("failed to notify state")
					return
				}

				if stage == common.PROD {
					// try to transition to the state it's supposed to be at
					payload, err := so.AppStore.GetRequestedState(app.AppKey, app.Stage)
					if err != nil {
						return
					}

					if latestAppState == common.FAILED {
						so.AppManager.incrementCrashLoop(payload)
					} else {
						so.AppManager.RequestAppState(payload)
					}
				}
			}

			// update the last recorded status
			lastKnownStatus = state.Status

			time.Sleep(pollingRate)
		}
	})

	return errorC
}

func (so *StateObserver) initObserverSpawner() chan error {
	messageC, errC := so.Container.ListenForContainerEvents(context.Background())

	errChan := make(chan error, 1)

	safe.Go(func() {
	loop:
		for {
			select {
			case event := <-messageC:
				switch event.Action {
				case "create", "start":
					containerName := event.Actor.Attributes["name"]

					expr := `(.{3})_([0-9]*)_.*` // our containerName convention e.g.: dev_1_testapp
					reg := regexp.MustCompile(expr)
					match := reg.FindStringSubmatch(containerName)

					// invalid container name, probably an intermediare container
					// or a container spawned by the user
					if len(match) == 0 {
						continue
					}

					stage, key, name, err := common.ParseContainerName(containerName)
					if err != nil {
						continue
					}

					so.addObserver(stage, key, name)
				}
			case err := <-errC:
				errChan <- err
				log.Error().Err(err).Msg("Error during observer spawner")
				close(errChan)
				break loop
			}
		}
	})

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

	so.initObserverSpawner()

	return err
}
