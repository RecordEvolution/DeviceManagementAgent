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

	"github.com/docker/docker/api/types/filters"
	"github.com/rs/zerolog/log"
)

type AppStateObserver struct {
	Stage   common.Stage
	AppKey  uint64
	AppName string
	errChan chan error
	// cancel stops the observer's polling goroutine. Removing an observer
	// only from the map used to leave its goroutine polling forever (the
	// container events spawner then added a fresh one on the next start
	// event, one leaked poller per restart cycle — observed as dozens of
	// concurrent docker CLI spawns pinning dockerd on small devices).
	cancel context.CancelFunc
	// ctx identifies the goroutine this map entry belongs to, so a dying
	// observer's deferred cleanup can prove it still owns the entry and
	// never tears down a replacement registered under the same name.
	ctx context.Context
}

// removeOwnObserver is the observers' deferred self-cleanup: it removes the
// map entry only when it still belongs to the calling goroutine (ownerCtx).
// The event spawner may have already replaced the entry with a fresh
// observer — that replacement must survive.
func (so *StateObserver) removeOwnObserver(mapKey string, ownerCtx context.Context) {
	so.observerMapMutex.Lock()
	observer := so.activeObservers[mapKey]
	if observer != nil && observer.ctx == ownerCtx {
		observer.cancel()
		delete(so.activeObservers, mapKey)
	}
	so.observerMapMutex.Unlock()
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

func (so *StateObserver) addComposeObserver(stage common.Stage, appKey uint64, appName string) (bool, error) {
	composeName := common.BuildComposeContainerName(stage, appKey, appName)

	// Existence check straight against the Docker API (compose labels its
	// containers with the project name) instead of shelling out a
	// `docker compose ls` — this runs on every container start event.
	listCtx, listCancel := context.WithTimeout(context.Background(), time.Second*30)
	containers, err := so.listComposeProjectContainers(listCtx, composeName)
	listCancel()
	if err != nil {
		return false, err
	}

	if len(containers) == 0 {
		log.Debug().Msgf("No compose was found for %s, not creating an observer...", composeName)
		return false, nil
	}

	createdObserver := false
	so.observerMapMutex.Lock()
	if so.activeObservers[composeName] == nil {
		ctx, cancel := context.WithCancel(context.Background())
		errC := so.observeComposeAppState(ctx, stage, appKey, appName)
		so.activeObservers[composeName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
			cancel:  cancel,
			ctx:     ctx,
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
		observerCtx, cancel := context.WithCancel(context.Background())
		errC := so.observeAppState(observerCtx, stage, appKey, appName)
		so.activeObservers[containerName] = &AppStateObserver{
			AppKey:  appKey,
			AppName: appName,
			Stage:   stage,
			errChan: errC,
			cancel:  cancel,
			ctx:     observerCtx,
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

	// A requested state can exist without a local app entry, e.g. when a removal
	// was requested while the app was still mid-transfer. Skip until the app is
	// registered through CreateOrUpdateApp.
	if app == nil {
		log.Warn().Msgf("no local app entry for requested compose app (%s, %s), skipping state correction", requestedState.AppName, requestedState.Stage)
		return nil
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

		if updateRemote {
			// Always sync with remote when online to ensure backend is up-to-date
			err = so.Notify(app, correctedAppState)
		} else if currentAppState != correctedAppState {
			err = so.NotifyLocal(app, correctedAppState)
		}

		if err != nil {
			return err
		}

		return nil
	}

	statusCtx, cancelStatus := context.WithTimeout(context.Background(), time.Second*30)
	containers, err := so.listComposeProjectContainers(statusCtx, composeName)
	cancelStatus()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to get container status for compose app %s", composeName)
		return err
	}

	latestAppState := aggregateContainerResults(containers)

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

		if app == nil {
			log.Warn().Msgf("no local app entry for requested app (%s, %s), skipping state correction", rState.AppName, rState.Stage)
			continue
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
			if updateRemote {
				// Always sync with remote when online to ensure backend is up-to-date.
				// This handles the case where the offline phase already corrected a transient
				// state (e.g. TRANSFERING → REMOVED) but the backend was never notified.
				err = so.Notify(app, correctedAppState)
			} else if currentAppState != correctedAppState {
				err = so.NotifyLocal(app, correctedAppState)
			}

			if err != nil {
				log.Error().Err(err).Msg("failed to notify app state")
			}

			// Throttle remote updates to avoid overwhelming the backend
			if updateRemote && idx < len(rStates)-1 {
				time.Sleep(100 * time.Millisecond)
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

// sleepCtx sleeps for d or until ctx is cancelled; it reports whether the
// caller should continue (false = cancelled, stop observing).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (so *StateObserver) observeAppState(observerCtx context.Context, stage common.Stage, appKey uint64, appName string) chan error {
	errorC := make(chan error, 1)
	pollingRate := time.Second * 1

	safe.Go(func() {
		lastKnownStatus := "UKNOWN"

		defer func() {
			so.removeOwnObserver(common.BuildContainerName(stage, appKey, appName), observerCtx)
		}()

		for {
			if observerCtx.Err() != nil {
				return
			}

			ctx := observerCtx
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
				if !sleepCtx(observerCtx, pollingRate) {
					return
				}
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
				if !sleepCtx(observerCtx, pollingRate) {
					return
				}
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

			if !sleepCtx(observerCtx, pollingRate) {
				return
			}
		}
	})

	return errorC
}

// listComposeProjectContainers returns every container (running or not) of a
// compose app in one Docker API call, via the project label docker compose
// stamps on all containers it creates. Replaces the former
// `docker compose ls` + `docker compose ps` CLI spawns: each of those cost
// two OS processes (docker CLI + compose plugin) and a full container
// enumeration in dockerd — polled every second per app, that alone pinned
// dockerd/containerd on small devices.
func (so *StateObserver) listComposeProjectContainers(ctx context.Context, composeAppName string) ([]container.ContainerResult, error) {
	project := common.NormalizeComposeProjectName(composeAppName)
	return so.Container.ListContainers(ctx, common.Dict{
		"all":     true,
		"filters": filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+project)),
	})
}

// aggregateContainerResults derives one app state from a compose project's
// containers: RUNNING when any container runs; otherwise FAILED when any
// exited non-zero; otherwise PRESENT. ListContainers already carries the
// exit code, so no per-container inspect calls are needed.
func aggregateContainerResults(containers []container.ContainerResult) common.AppState {
	aggregatedState := common.PRESENT
	for _, cont := range containers {
		if cont.State == "running" {
			return common.RUNNING
		}

		if cont.State == "exited" && cont.ExitCode > 0 {
			aggregatedState = common.FAILED
		}
	}

	return aggregatedState
}

func (so *StateObserver) observeComposeAppState(observerCtx context.Context, stage common.Stage, appKey uint64, appName string) chan error {
	errorC := make(chan error, 1)
	// Same cadence as the plain-container observers. A tick is one
	// label-filtered ListContainers API call served from dockerd's in-memory
	// store — cheap enough for 1s. (The old implementation shelled out
	// `docker compose ls` + `docker compose ps` per tick — four OS processes
	// per app per second, which pinned dockerd on small devices; never poll
	// via the compose CLI here again.)
	pollingRate := time.Second * 1

	safe.Go(func() {
		var lastKnownStatus common.AppState
		composeAppName := common.BuildComposeContainerName(stage, appKey, appName)

		defer func() {
			so.removeOwnObserver(composeAppName, observerCtx)
		}()

		for {
			if observerCtx.Err() != nil {
				return
			}

			listCtx, cancelList := context.WithTimeout(observerCtx, time.Second*30)
			containers, err := so.listComposeProjectContainers(listCtx, composeAppName)
			cancelList()
			if err != nil {
				if observerCtx.Err() != nil {
					return
				}

				// Transient daemon errors must not hot-loop: the old CLI
				// variant re-spawned processes back-to-back on this path.
				log.Error().Err(err).Msgf("Failed to list containers for compose app %s", composeAppName)
				if !sleepCtx(observerCtx, pollingRate) {
					return
				}
				continue
			}

			if len(containers) == 0 {
				// Project has no containers anymore (torn down or removed);
				// the events spawner recreates an observer when they return.
				return
			}

			latestAppState := aggregateContainerResults(containers)

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
				if !sleepCtx(observerCtx, pollingRate) {
					return
				}
				continue
			} else {
				app.UnlockTransition()
			}

			// status change detected
			// always executed the state check on init
			if lastKnownStatus == latestAppState && latestAppState == curAppState {
				// log.Debug().Msgf("app (%s, %s) container status remains unchanged: %s", stage, appName, lastKnownStatus)
				if !sleepCtx(observerCtx, pollingRate) {
					return
				}
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

			if !sleepCtx(observerCtx, pollingRate) {
				return
			}
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
				// die/kill/destroy events used to tear the observer down here.
				// That is redundant — observers self-terminate once the
				// container (or the compose project's last container) is gone,
				// and a stopped-but-existing container must STAY observed so
				// its FAILED/exited state is still noticed and reported. Worse,
				// on every service restart the remove+re-add pair leaked the
				// old polling goroutine (remove never stopped it), stacking up
				// concurrent pollers until dockerd was pinned. Only create /
				// start events matter to the spawner.
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
			// addComposeObserver checks via the Docker API whether the app
			// has a compose project at all and no-ops when it does not.
			_, err := so.addComposeObserver(app.Stage, app.AppKey, app.AppName)
			if err != nil {
				log.Error().Err(err).Msgf("failed to add compose observer for %s (%s)", app.AppName, app.Stage)
			}
		}

	}

	if !so.spawnerActive {
		so.initObserverSpawner()
	}

	return err
}
