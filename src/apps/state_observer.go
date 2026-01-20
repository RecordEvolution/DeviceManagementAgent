package apps

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/logging"
	"reagent/safe"
	"reagent/store"
	"regexp"
	"strings"
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
	LogManager       *logging.LogManager
	AppManager       *AppManager
	Container        container.Container
	activeObservers  map[string]*AppStateObserver
	spawnerActive    bool
	observerMapMutex sync.Mutex
}

func NewObserver(container container.Container, appStore *store.AppStore, logManager *logging.LogManager) StateObserver {
	return StateObserver{
		Container:       container,
		AppStore:        appStore,
		LogManager:      logManager,
		activeObservers: make(map[string]*AppStateObserver),
	}
}

func (so *StateObserver) NotifyLocal(app *common.App, achievedState common.AppState) error {
	// update in memory
	app.StateLock.Lock()
	app.CurrentState = achievedState
	appKey := app.AppKey
	stage := app.Stage
	appName := app.AppName
	app.StateLock.Unlock()

	// First update the local app state
	err := so.AppStore.UpdateLocalAppState(app, achievedState)
	if err != nil {
		return err
	}

	// If app reached REMOVED state, check if backend requested removal before cleaning up database
	if achievedState == common.REMOVED {
		// Check what the backend requested
		requestedState, err := so.AppStore.GetRequestedState(appKey, stage)
		if err != nil {
			// If there's no requested state, don't delete
			log.Debug().
				Str("app", appName).
				Uint64("app_key", appKey).
				Str("stage", string(stage)).
				Msg("No requested state found, keeping database entries")
			return nil
		}

		// Only delete from database if backend requested REMOVED or UNINSTALLED
		if requestedState.RequestedState == common.REMOVED || requestedState.RequestedState == common.UNINSTALLED {
			log.Info().
				Str("app", appName).
				Uint64("app_key", appKey).
				Str("stage", string(stage)).
				Str("requested_state", string(requestedState.RequestedState)).
				Msg("🗑️ App reached REMOVED state and backend requested removal, cleaning up database entries")

			// Delete from database
			err = so.AppStore.DeleteAppState(appKey, stage)
			if err != nil {
				log.Error().Stack().Err(err).Msg("Failed to delete app state from database")
			}

			err = so.AppStore.DeleteRequestedState(appKey, stage)
			if err != nil {
				log.Error().Stack().Err(err).Msg("Failed to delete requested state from database")
			}
		} else {
			log.Debug().
				Str("app", appName).
				Uint64("app_key", appKey).
				Str("stage", string(stage)).
				Str("requested_state", string(requestedState.RequestedState)).
				Msg("App reached REMOVED but backend requested different state, keeping database entries")
		}
	}

	return nil
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

func (so *StateObserver) removeObserver(stage common.Stage, appKey uint64, appName string) bool {
	containerName := common.BuildContainerName(stage, appKey, appName)
	so.observerMapMutex.Lock()
	observer := so.activeObservers[containerName]

	deletedObserver := false
	if observer != nil {
		close(observer.errChan)
		delete(so.activeObservers, containerName)
		deletedObserver = true

		log.Debug().Msgf("removed an observer for %s (%s)", appName, stage)
	}
	so.observerMapMutex.Unlock()

	return deletedObserver
}

func (so *StateObserver) removeComposeObserver(stage common.Stage, appKey uint64, appName string) bool {
	composeAppName := common.BuildComposeContainerName(stage, appKey, appName)
	so.observerMapMutex.Lock()
	observer := so.activeObservers[composeAppName]

	deletedObserver := false
	if observer != nil {
		close(observer.errChan)
		delete(so.activeObservers, composeAppName)
		deletedObserver = true
		log.Debug().Msgf("removed a compose observer for %s (%s)", appName, stage)
	}
	so.observerMapMutex.Unlock()

	return deletedObserver
}

func (so *StateObserver) addComposeObserver(stage common.Stage, appKey uint64, appName string) (bool, error) {
	composeName := common.BuildComposeContainerName(stage, appKey, appName)
	compose := so.Container.Compose()
	composeEntryList, err := compose.List()
	if err != nil {
		return false, err
	}

	var foundComposeEntry *container.ComposeListEntry
	for _, composeEntry := range composeEntryList {
		if composeEntry.Name == composeName {
			foundComposeEntry = &composeEntry
			break
		}
	}

	if foundComposeEntry == nil {
		log.Debug().Msgf("No compose was found for %s, not creating an observer...", composeName)
		return false, nil
	}

	createdObserver := false
	so.observerMapMutex.Lock()
	if so.activeObservers[composeName] == nil {
		errC := so.observeComposeAppState(stage, appKey, appName)
		so.activeObservers[composeName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
		}
		createdObserver = true

		log.Debug().Msgf("created a compose observer for %s (%s)", appName, stage)
	}
	so.observerMapMutex.Unlock()

	return createdObserver, nil
}

func (so *StateObserver) addObserver(stage common.Stage, appKey uint64, appName string) bool {
	containerName := common.BuildContainerName(stage, appKey, appName)

	ctx := context.Background()
	_, err := so.Container.GetContainerState(ctx, containerName)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return false
		}

		log.Debug().Msgf("No container was found for %s, not creating an observer...", containerName)
		return false
	}

	createdObserver := false
	so.observerMapMutex.Lock()
	if so.activeObservers[containerName] == nil {
		errC := so.observeAppState(stage, appKey, appName)
		so.activeObservers[containerName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
		}
		createdObserver = true
		log.Debug().Msgf("created an observer for %s (%s)", appName, stage)
	}

	so.observerMapMutex.Unlock()

	return createdObserver
}

func (so *StateObserver) CorrectComposeAppState(requestedState common.TransitionPayload, images []container.ImageResult, composeListEntry []container.ComposeListEntry, updateRemote bool) error {
	compose := so.Container.Compose()
	composeName := common.BuildComposeContainerName(requestedState.Stage, requestedState.AppKey, requestedState.AppName)
	app, err := so.AppStore.GetApp(requestedState.AppKey, requestedState.Stage)
	if err != nil {
		return err
	}

	var foundComposeEntry *container.ComposeListEntry
	for _, composeEntry := range composeListEntry {
		if composeEntry.Name == composeName {
			foundComposeEntry = &composeEntry
		}
	}

	// NEVER STARTED / REMOVED / STOPPED (no containers available)
	if foundComposeEntry == nil {
		app.StateLock.Lock()
		currentAppState := app.CurrentState
		app.StateLock.Unlock()

		// Need to get images
		services, ok := (requestedState.DockerCompose["services"]).(map[string]interface{})
		if !ok {
			return errors.New("failed to infer services")
		}

		foundAllImages := true
		for _, serviceInterface := range services {
			service, ok := (serviceInterface).(map[string]interface{})
			if !ok {
				return errors.New("failed to infer service")
			}

			if service["image"] != nil {
				imageName := fmt.Sprint(service["image"])

				foundImage := false
				for _, image := range images {
					for _, repoTag := range image.RepoTags {
						if strings.Contains(repoTag, imageName) {
							foundImage = true
							break
						}
					}
				}

				if !foundImage {
					foundAllImages = false
					break
				}

			}
		}

		hasComposeDir := compose.HasComposeDir(app.AppName, app.Stage)

		correctedAppState := common.UNINSTALLED
		// no images were found (and no container) for this guy, so this guy is REMOVED
		if !hasComposeDir {
			if currentAppState == common.UNINSTALLED {
				correctedAppState = common.UNINSTALLED
			} else {
				correctedAppState = common.REMOVED
			}
		} else if foundAllImages {
			// images were found for this guy, but no container, this means --> PRESENT
			correctedAppState = common.PRESENT
		}

		if currentAppState != correctedAppState {
			if updateRemote {
				// notify the remote database of any changed states due to correction
				err = so.Notify(app, correctedAppState)
			} else {
				err = so.NotifyLocal(app, correctedAppState)
			}
		}

		if err != nil {
			return err
		}

		return nil
	}

	containerStatuses, err := compose.Status(foundComposeEntry.ConfigFiles)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to get container status for compose app with config %s", foundComposeEntry.ConfigFiles)
		return err
	}

	latestAppState, err := so.aggregateStatuses(containerStatuses)
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
		correctedAppState = latestAppState
	default:
		correctedAppState = latestAppState
	}

	if updateRemote {
		err = so.Notify(app, correctedAppState)
	} else {
		err = so.NotifyLocal(app, correctedAppState)
	}

	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to notify state change")
	}

	return nil
}

// CorrectAppStates ensures that the app state corresponds with the container status of the app.
// Any transient states will be handled accordingly. After the states have been assured, it will attempt to update the app states remotely if true is passed.
func (so *StateObserver) CorrectAppStates(updateRemote bool) error {

	log.Debug().Msgf("Getting requested states (UpdateRemote: %t)", updateRemote)
	rStates, err := so.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}
	log.Debug().Msg("Got requested states")

	log.Debug().Msg("Get all local images for next step")
	allImages, err := so.Container.GetImages(context.Background(), "")
	if err != nil {
		return err
	}

	log.Debug().Msg("Done getting all local images for next step")

	compose := so.Container.Compose()
	composeListEntry, err := compose.List()
	if err != nil {
		return err
	}

	for idx, rState := range rStates {

		// ComposeApp
		if rState.DockerCompose != nil {
			err = so.CorrectComposeAppState(rState, allImages, composeListEntry, updateRemote)
			if err != nil {
				return err
			}

			// Throttle remote updates to avoid overwhelming the backend
			if updateRemote && idx < len(rStates)-1 {
				time.Sleep(100 * time.Millisecond)
			}

			continue
		}

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

			// Since the error is a container not found error (which is expected), we set the err to nil again
			err = nil

			// we should check if the image exists, if it does not, we should set the state to 'REMOVED', else to 'STOPPED'
			var fullImageName string
			if rState.Stage == common.DEV {
				fullImageName = rState.RegistryImageName.Dev
			} else if rState.Stage == common.PROD {
				fullImageName = rState.RegistryImageName.Prod
			}

			foundImage := false
			for _, image := range allImages {
				if len(image.RepoTags) > 0 && strings.Contains(image.RepoTags[0], fullImageName) {
					foundImage = true
					break
				}
			}

			app.StateLock.Lock()
			currentAppState := app.CurrentState
			var correctedAppState common.AppState

			// Check for stuck transient states first
			switch currentAppState {
			case common.DOWNLOADING,
				common.TRANSFERING,
				common.PUBLISHING,
				common.BUILDING,
				common.UPDATING:
				// Transient states: if image exists → PRESENT, else REMOVED
				if foundImage {
					correctedAppState = common.PRESENT
				} else {
					correctedAppState = common.REMOVED
				}
			default:
				// no images were found (and no container) for this guy, so this guy is REMOVED
				if !foundImage {
					if currentAppState == common.UNINSTALLED {
						correctedAppState = common.UNINSTALLED
					} else {
						correctedAppState = common.REMOVED
					}
				} else {
					// images were found for this guy, but no container, this means --> PRESENT
					correctedAppState = common.PRESENT
				}
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
			log.Error().Stack().Err(err).Msg("Failed to notify state change")
		}

		// Throttle remote updates to avoid overwhelming the backend
		if updateRemote && idx < len(rStates)-1 {
			time.Sleep(100 * time.Millisecond)
		}

	}

	return nil
}

func (so *StateObserver) observeAppState(stage common.Stage, appKey uint64, appName string) chan error {
	errorC := make(chan error, 1)
	pollingRate := time.Second * 1

	safe.Go(func() {
		lastKnownStatus := "UKNOWN"

		defer func() {
			so.removeObserver(stage, appKey, appName)
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

				// TODO: check if the following makes any sense??
				// app.StateLock.Lock()
				// app.CurrentState = common.PRESENT
				// app.StateLock.Unlock()

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

			alreadyTransitioning := app.SecureTransition()
			if alreadyTransitioning {
				log.Debug().Msg("State observer: app is already transitioning, waiting for transition to finish...")
				time.Sleep(pollingRate)
				continue
			} else {
				app.UnlockTransition()
			}

			if curAppState != latestAppState && !common.IsTransientState(curAppState) && !common.IsTransientState(latestAppState) {
				log.Debug().Msgf("app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("app (%s, %s) updating from %s to %s", appName, stage, curAppState, latestAppState)

				// update the current local and remote app state
				err = so.Notify(app, latestAppState)
				if err != nil {
					errorC <- err
					log.Error().Err(err).Msg("failed to notify state")
					return
				}

				if latestAppState == common.FAILED && (stage == common.DEV || stage == common.PROD) {
					err = so.LogManager.Write(containerName, fmt.Sprintf("%s (%s) exited with status code: %d", appName, stage, state.ExitCode))
					if err != nil {
						log.Error().Err(err).Msgf("failed to publish exit message to container %s", containerName)
					}
				}

				if stage == common.PROD {
					// try to transition to the state it's supposed to be at
					payload, err := so.AppStore.GetRequestedState(app.AppKey, app.Stage)
					if err != nil {
						return
					}

					if latestAppState == common.FAILED {
						retries, sleepTime := so.AppManager.incrementCrashLoop(payload)
						err = so.LogManager.Write(containerName, fmt.Sprintf("Entered a crashloop (%s attempt), retrying in %s", common.Ordinal(retries), sleepTime))
						if err != nil {
							log.Error().Err(err).Msgf("failed to publish retry message to container %s", containerName)
						}

						return
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

func (so *StateObserver) aggregateStatuses(containerStatuses []container.ComposeStatus) (common.AppState, error) {
	notRunningContainers := make([]string, 0)
	for _, status := range containerStatuses {
		// If one container is running, we say that it is running
		if status.State == "running" {
			return common.RUNNING, nil
		} else {
			notRunningContainers = append(notRunningContainers, status.Name)
		}
	}

	aggregatedState := common.PRESENT
	for _, notRunningContainer := range notRunningContainers {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		state, err := so.Container.GetContainerState(ctx, notRunningContainer)
		if err != nil {
			cancel()
			return "", err
		}

		cancel()

		if state.Status == "exited" && state.ExitCode > 0 {
			aggregatedState = common.FAILED
		}
	}

	return aggregatedState, nil
}

func (so *StateObserver) observeComposeAppState(stage common.Stage, appKey uint64, appName string) chan error {
	errorC := make(chan error, 1)
	pollingRate := time.Second * 1

	safe.Go(func() {
		var lastKnownStatus common.AppState

		defer func() {
			so.removeComposeObserver(stage, appKey, appName)
		}()

		for {
			composeAppName := common.BuildComposeContainerName(stage, appKey, appName)
			compose := so.Container.Compose()
			composeEntryList, err := compose.List()
			if err != nil {
				return
			}

			var foundComposeEntry *container.ComposeListEntry
			for _, composeEntry := range composeEntryList {
				if composeEntry.Name == composeAppName {
					foundComposeEntry = &composeEntry
					break
				}
			}

			if foundComposeEntry == nil {
				app, err := so.AppStore.GetApp(appKey, stage)
				if err != nil {
					log.Error().Err(err).Msg("failed to get app")
					return
				}

				if app == nil {
					return
				}

				// TODO: check if this actually makes sense, what if it's being deleted?
				// app.StateLock.Lock()
				// app.CurrentState = common.PRESENT
				// app.StateLock.Unlock()

				// log.Debug().Msgf("No container was found for compose app %s, removing observer..", composeAppName)
				return
			}

			containerStatuses, err := compose.Status(foundComposeEntry.ConfigFiles)
			if err != nil {
				log.Error().Err(err).Msgf("Failed to get container status for compose app with config %s", foundComposeEntry.ConfigFiles)
				continue
			}

			latestAppState, err := so.aggregateStatuses(containerStatuses)
			if err != nil {
				continue
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

			alreadyTransitioning := app.SecureTransition()
			if alreadyTransitioning {
				log.Debug().Msg("State observer: compose app is already transitioning, waiting for transition to finish...")
				time.Sleep(pollingRate)
				continue
			} else {
				app.UnlockTransition()
			}

			// status change detected
			// always executed the state check on init
			if lastKnownStatus == latestAppState && latestAppState == curAppState {
				// log.Debug().Msgf("app (%s, %s) container status remains unchanged: %s", stage, appName, lastKnownStatus)
				time.Sleep(pollingRate)
				continue
			}

			if curAppState != latestAppState && !common.IsTransientState(curAppState) {
				log.Debug().Msgf("app (%s, %s) state is not up to date", appName, stage)
				log.Debug().Msgf("app (%s, %s) updating from %s to %s", appName, stage, curAppState, latestAppState)

				// update the current local and remote app state
				err = so.Notify(app, latestAppState)
				if err != nil {
					errorC <- err
					log.Error().Err(err).Msg("failed to notify state")
					return
				}

				containerTopic := common.BuildContainerName(stage, appKey, appName)

				if stage == common.PROD {
					// try to transition to the state it's supposed to be at
					payload, err := so.AppStore.GetRequestedState(app.AppKey, app.Stage)
					if err != nil {
						return
					}

					if latestAppState == common.FAILED {
						retries, sleepTime := so.AppManager.incrementCrashLoop(payload)
						err = so.LogManager.Write(containerTopic, fmt.Sprintf("Entered a crashloop (%s attempt), retrying in %s", common.Ordinal(retries), sleepTime))
						if err != nil {
							log.Error().Err(err).Msgf("failed to publish retry message to container %s", containerTopic)
						}

						return
					} else {
						so.AppManager.RequestAppState(payload)
					}
				}
			}

			// update the last recorded status
			lastKnownStatus = latestAppState

			time.Sleep(pollingRate)
		}
	})

	return errorC
}

var ContainerNameRegexExp = regexp.MustCompile(`(.{3,4})_([0-9]*)_.*`)
var ComposeContainerNameRegexExp = regexp.MustCompile(`(dev|prod)_(\d+)_([a-zA-Z0-9]+)_compose-([a-zA-Z0-9]+)-(\d+)`)

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
					match := ContainerNameRegexExp.FindStringSubmatch(containerName)
					matchCompose := ComposeContainerNameRegexExp.FindStringSubmatch(containerName)

					if len(matchCompose) != 0 {

						// Compose container looks as follows:
						// dev_1336_markopetzoldomaewamoushindeiru_compose-web-1
						// dev_1336_markopetzoldomaewamoushindeiru_compose-db-1
						// We only need the first part to identify the compose
						composeContainerName := strings.Split(containerName, "-")[0]
						stage, key, name, err := common.ParseComposeContainerName(composeContainerName)
						if err != nil {
							continue
						}

						so.removeComposeObserver(stage, key, name)

						continue
					}

					if len(match) != 0 {
						stage, key, name, err := common.ParseContainerName(containerName)
						if err != nil {
							continue
						}

						so.removeObserver(stage, key, name)
					}

				case "create", "start":
					containerName := event.Actor.Attributes["name"]
					match := ContainerNameRegexExp.FindStringSubmatch(containerName)
					matchCompose := ComposeContainerNameRegexExp.FindStringSubmatch(containerName)

					if len(matchCompose) != 0 {
						composeContainerName := strings.Split(containerName, "-")[0]
						stage, key, name, err := common.ParseComposeContainerName(composeContainerName)
						if err != nil {
							continue
						}

						so.addComposeObserver(stage, key, name)

						continue
					}

					if len(match) != 0 {
						stage, key, name, err := common.ParseContainerName(containerName)
						if err != nil {
							continue
						}
						so.addObserver(stage, key, name)
					}

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

	compose := so.Container.Compose()
	composeListEntry, err := compose.List()
	if err != nil {
		return err
	}

	for _, app := range apps {
		// Only create observers for apps that should have containers running
		// Skip apps in REMOVED, UNINSTALLED, FAILED, BUILT, PUBLISHED states
		app.StateLock.Lock()
		currentState := app.CurrentState
		app.StateLock.Unlock()

		if currentState == common.REMOVED ||
			currentState == common.UNINSTALLED ||
			currentState == common.FAILED ||
			currentState == common.BUILT ||
			currentState == common.PUBLISHED {
			continue
		}

		createdObserver := so.addObserver(app.Stage, app.AppKey, app.AppName)

		if !createdObserver {
			var composeApp *container.ComposeListEntry
			composeAppName := common.BuildComposeContainerName(app.Stage, app.AppKey, app.AppName)
			for _, composeEntry := range composeListEntry {
				if composeEntry.Name == composeAppName {
					composeApp = &composeEntry
					break
				}
			}

			if composeApp != nil {
				so.addComposeObserver(app.Stage, app.AppKey, app.AppName)
			}
		}

	}

	if !so.spawnerActive {
		so.initObserverSpawner()
	}

	return err
}
