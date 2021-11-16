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
	AppStore         *store.AppStore
	AppManager       *AppManager
	Container        container.Container
	activeObservers  map[string]*AppStateObserver
	spawnerActive    bool
	observerMapMutex sync.Mutex
}

func NewObserver(container container.Container, appStore *store.AppStore) StateObserver {
	return StateObserver{
		Container:       container,
		AppStore:        appStore,
		activeObservers: make(map[string]*AppStateObserver),
	}
}

func (so *StateObserver) NotifyLocal(app *common.App, achievedState common.AppState) error {
	// update in memory
	app.StateLock.Lock()
	app.CurrentState = achievedState
	app.StateLock.Unlock()

	return so.AppStore.UpdateLocalAppState(app, achievedState)
}

func (so *StateObserver) NotifyRemote(app *common.App, achievedState common.AppState) error {
	ctx := context.Background()
	return so.AppStore.UpdateRemoteAppState(ctx, app, achievedState)
}

// Notify is used by the StateMachine to notify the observer that the app state has changed
func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {
	err := so.NotifyLocal(app, achievedState)
	if err != nil {
		return err
	}

	return so.NotifyRemote(app, achievedState)
}

func (so *StateObserver) removeObserver(stage common.Stage, appKey uint64, appName string) {
	containerName := common.BuildContainerName(stage, appKey, appName)
	so.observerMapMutex.Lock()
	observer := so.activeObservers[containerName]

	if observer != nil {
		close(observer.errChan)
		delete(so.activeObservers, containerName)
		log.Debug().Msgf("removed an observer for %s (%s)", appName, stage)
	}
	so.observerMapMutex.Unlock()
}

func (so *StateObserver) addObserver(stage common.Stage, appKey uint64, appName string) {
	containerName := common.BuildContainerName(stage, appKey, appName)

	ctx := context.Background()
	_, err := so.Container.GetContainerState(ctx, containerName)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return
		}

		log.Debug().Msgf("No container was found for %s, not creating an observer...", containerName)
		return
	}

	so.observerMapMutex.Lock()
	if so.activeObservers[containerName] == nil {
		errC := so.observeAppState(stage, appKey, appName)
		log.Debug().Msgf("created an observer for %s (%s)", appName, stage)
		so.activeObservers[containerName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
		}
	}
	so.observerMapMutex.Unlock()
}

// CorrectAppStates ensures that the app state corresponds with the container status of the app.
// Any transient states will be handled accordingly. After the states have been assured, it will attempt to update the app states remotely if true is passed.
func (so *StateObserver) CorrectAppStates(updateRemote bool) error {

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
			if !errdefs.IsContainerNotFound(err) {
				return err
			}

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
			currentAppState := app.CurrentState
			var correctedAppState common.AppState
			// no images were found (and no container) for this guy, so this guy is REMOVED
			if len(images) == 0 {
				if currentAppState == common.UNINSTALLED {
					correctedAppState = common.UNINSTALLED
				} else {
					correctedAppState = common.REMOVED
				}
			} else {
				// images were found for this guy, but no container, this means --> PRESENT
				correctedAppState = common.PRESENT
			}

			app.StateLock.Unlock()
			if currentAppState != correctedAppState {
				if updateRemote {
					// notify the remote database of any changed states due to correction
					err = so.Notify(app, correctedAppState)
				} else {
					err = so.NotifyLocal(app, correctedAppState)
				}
			}

			if err != nil {
				log.Error().Err(err).Msg("failed to notify app state")
			}

			// should be all good iterate over next app
			continue
		}

		appStateDeterminedByContainer, err := common.ContainerStateToAppState(container.Status, int(container.ExitCode))
		if err != nil {
			return err
		}

		app.StateLock.Lock()
		currentAppState := app.CurrentState
		app.StateLock.Unlock()

		var correctedAppState common.AppState
		switch currentAppState {
		case common.DOWNLOADING,
			common.TRANSFERING,
			common.BUILDING,
			common.PUBLISHING:
			correctedAppState = common.REMOVED
		case common.UPDATING:
			correctedAppState = common.PRESENT
		case common.STOPPING,
			common.STARTING:
			correctedAppState = appStateDeterminedByContainer
		default:
			correctedAppState = appStateDeterminedByContainer
		}

		if updateRemote {
			err = so.Notify(app, correctedAppState)
		} else {
			err = so.NotifyLocal(app, correctedAppState)
		}

		if err != nil {
			log.Error().Stack().Err(err).Msg("Failed to notifiy state change")
		}

	}

	return nil
}

func (so *StateObserver) observeAppState(stage common.Stage, appKey uint64, appName string) chan error {
	errorC := make(chan error, 1)
	pollingRate := time.Second * 5

	safe.Go(func() {
		lastKnownStatus := "UKNOWN"

		defer func() {
			so.removeObserver(stage, appKey, appName)

			// try to transition to the state it's supposed to be at
			payload, err := so.AppStore.GetRequestedState(appKey, stage)
			if err != nil {
				log.Error().Err(err).Msg("failed to get requested state")
				return
			}

			err = so.AppManager.RequestAppState(payload)
			if err != nil {
				log.Error().Err(err).Msg("failed to request app state")
			}

		}()

		for {
			ctx := context.Background()
			containerName := common.BuildContainerName(stage, appKey, appName)

			state, err := so.Container.GetContainerState(ctx, containerName)
			if err != nil {
				if !errdefs.IsContainerNotFound(err) {
					errorC <- err
					return
				}

				// if the container doesn't exist anymore, need to make sure app is in the stopped state
				app, err := so.AppStore.GetApp(appKey, stage)
				if err != nil {
					log.Error().Err(err).Msg("failed to get app")
					return
				}

				if app == nil {
					return
				}

				app.StateLock.Lock()
				app.CurrentState = common.PRESENT
				app.StateLock.Unlock()

				log.Debug().Msgf("No container was found for %s, removing observer..", containerName)
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

			if app == nil {
				return
			}

			app.StateLock.Lock()
			curAppState := app.CurrentState
			app.StateLock.Unlock()

			if curAppState != latestAppState && !common.IsTransientState(curAppState) && !common.IsTransientState(latestAppState) {
				log.Debug().Msgf("app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("app (%s, %s) updating from %s to %s", appName, stage, curAppState, latestAppState)

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
				case "die", "kill", "destroy":
					containerName := event.Actor.Attributes["name"]
					reg := regexp.MustCompile(common.ContainerNameRegExp)
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

					so.removeObserver(stage, key, name)
				case "create", "start":
					containerName := event.Actor.Attributes["name"]
					reg := regexp.MustCompile(common.ContainerNameRegExp)
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

				so.spawnerActive = false
				break loop
			}
		}
	})

	so.spawnerActive = true
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

	if !so.spawnerActive {
		so.initObserverSpawner()
	}

	return err
}
