package apps

import (
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"reagent/safe"
	"reagent/store"
	"reagent/tunnel"
	"sync"

	"github.com/rs/zerolog/log"
)

type AppManager struct {
	AppStore         *store.AppStore
	StateMachine     *StateMachine
	StateObserver    *StateObserver
	AppTunnelManager tunnel.AppTunnelManager
	crashLoops       map[*CrashLoop]struct{}
	crashLoopLock    sync.Mutex
}

func NewAppManager(sm *StateMachine, as *store.AppStore, so *StateObserver, tm tunnel.AppTunnelManager) *AppManager {
	am := AppManager{
		StateMachine:     sm,
		StateObserver:    so,
		AppStore:         as,
		AppTunnelManager: tm,
		crashLoops:       make(map[*CrashLoop]struct{}),
	}

	am.StateObserver.AppManager = &am
	return &am
}

func (am *AppManager) syncPortState(payload common.TransitionPayload, app *common.App) error {
	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	// handle app tunnels
	portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
	if err != nil {
		return err
	}

	for _, portRule := range portRules {
		appTunnel, _ := am.AppTunnelManager.GetAppTunnel(app.AppKey)

		// port creation
		if portRule.Public {
			// if the app is currently running, spawn a tunnel, else just register it for later spawnage
			if curAppState == common.RUNNING {
				if appTunnel == nil {
					log.Debug().Msgf("Creating app tunnel for app: %d", app.AppKey)
					_, err = am.AppTunnelManager.CreateAppTunnel(portRule.AppKey, portRule.DeviceKey, portRule.Port, portRule.Protocol, "")
					if err != nil {
						return err
					}
				} else {
					if !appTunnel.Running {
						log.Debug().Msgf("Activating app tunnel for app: %d", app.AppKey)
						err = am.AppTunnelManager.ActivateAppTunnel(appTunnel)
						if err != nil {
							log.Error().Err(err).Msgf("failed to activate app tunnel for %d", app.AppKey)
						}
					}
				}
			} else {
				if appTunnel == nil {
					log.Debug().Msgf("Registering app tunnel for app: %d", app.AppKey)
					am.AppTunnelManager.RegisterAppTunnel(portRule.AppKey, portRule.DeviceKey, portRule.Port, portRule.Protocol, "")
				} else {
					if appTunnel.Running {
						log.Debug().Msgf("Deactivating app tunnel for app: %d", app.AppKey)
						am.AppTunnelManager.DeactivateAppTunnel(appTunnel)
					}
				}
			}

			// port deletion
		} else {
			if appTunnel != nil {
				log.Debug().Msgf("Killing app tunnel for app: %d", app.AppKey)
				err = am.AppTunnelManager.KillAppTunnel(portRule.AppKey)
				if err != nil {
					log.Error().Err(err).Msgf("failed to kill app tunnel for %d", app.AppKey)
				}
			}
		}

	}

	return nil
}

func (am *AppManager) RequestAppState(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	requestedAppState := app.RequestedState
	app.StateLock.Unlock()

	log.Debug().Msgf("Received Requested State (FROM %s to) %s for %s (%s)", curAppState, requestedAppState, payload.AppName, payload.Stage)

	// clear crashloop counter if changing state request
	if !payload.Retrying {
		am.clearCrashLoop(app.AppKey, app.Stage)
	}

	// TODO: get rid of this ugly patch: cancel any filetransfers for this container on stop press
	if (curAppState == common.REMOVED || curAppState == common.PRESENT || curAppState == common.FAILED) &&
		(requestedAppState == common.PRESENT || requestedAppState == common.BUILT) && payload.Stage == common.DEV {
		am.StateMachine.Filesystem.CancelFileTransfer(payload.ContainerName.Dev)
	}

	builtState := curAppState == common.BUILT && requestedAppState == common.BUILT
	if curAppState == requestedAppState && !payload.RequestUpdate && !builtState {

		// sync ports even when app is already in the same state
		err = am.syncPortState(payload, app)
		if err != nil {
			log.Error().Err(err).Msgf("failed to sync port state")
			return err
		}

		log.Debug().Msgf("app %s (%s) is already on latest state (%s)", app.AppName, app.Stage, requestedAppState)
		return nil
	}

	if payload.CancelTransition {
		log.Debug().Msgf("Cancel request was received for %s (%s) (currently: %s)", app.AppName, app.Stage, app.CurrentState)
		am.StateMachine.CancelTransition(app, payload)
		return nil
	}

	locked := app.SecureTransition() // if the app is not locked, it will lock the app
	if locked {
		log.Warn().Msgf("App with name %s and stage %s is already transitioning", app.AppName, app.Stage)
		return nil
	}

	// need to call this after we have secured the lock
	// to not change the actual state in the middle of an ongoing transition
	// this is necessary because some state transitions require a change of actual state (BUILD & PUBLISH)
	err = am.UpdateCurrentAppState(payload)
	if err != nil {
		app.UnlockTransition()
		return err
	}

	// before we transition, should request the token
	token, err := am.AppStore.GetRegistryToken(payload.RequestorAccountKey)
	if err != nil {
		app.UnlockTransition()
		return err
	}

	payload.RegisteryToken = token

	// if there's an active crashloopbackoff and we are no longer transitioning to running state, clear the loop
	// if payload.Stage == common.PROD && payload.RequestedState != common.RUNNING {
	// 	am.clearCrashLoop(payload.AppKey, payload.Stage)
	// }

	errC := am.StateMachine.InitTransition(app, payload)
	if errC == nil {
		// not yet implemented or nullified state transition
		app.UnlockTransition()
		return nil
	}

	// block till transition has finished
	select {
	case err := <-errC:
		app.UnlockTransition()

		if errdefs.IsNoActionTransition(err) {
			log.Debug().Msg("A no action transition was executed, nothing to do. Will also not verify")
			return nil
		}

		isCanceled := errdefs.IsDockerStreamCanceled(err)
		if err == nil || isCanceled {
			if !isCanceled {
				log.Info().Msgf("Successfully finished transaction for App (%s, %s)", app.AppName, app.Stage)
			} else {
				log.Info().Msgf("Successfully canceled transition for App (%s, %s)", app.AppName, app.Stage)
			}

			// Verify if app has the latest requested state
			// TODO: properly handle it when verifying fails
			err := am.VerifyState(app)
			if err != nil {
				log.Error().Err(err).Msgf("failed to verify app state")
				return err
			}
			return nil
		}

		// If anything goes wrong with the transition function
		// we should set the state change to FAILED
		// This will in turn update the in memory state and the local database state
		// which will in turn update the remote database as well
		if err != nil {
			setStateErr := am.StateObserver.Notify(app, common.FAILED)
			if setStateErr != nil {
				// wrap errors into one
				err = fmt.Errorf("failed to complete transition: %w", err)
			}
		}

		log.Error().Msgf("An error occured during transition from %s to %s for %s (%s)", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
		log.Error().Err(err).Msgf("The app state for %s (%s) has been set to FAILED", app.AppName, app.Stage)

		// enter the crashloop when we encounter a FAILED state
		if payload.Stage == common.PROD {
			am.incrementCrashLoop(payload)
		}
	}

	return nil
}

func IsInvalidOfflineTransition(app *common.App, payload common.TransitionPayload) bool {
	notInstalled := app.CurrentState == common.REMOVED || app.CurrentState == common.UNINSTALLED
	removalRequest := payload.RequestedState == common.REMOVED || payload.RequestedState == common.UNINSTALLED

	// if the app is not on the device and we do any transition that would require internet we return true
	if notInstalled && payload.Stage == common.PROD && !removalRequest {
		return true
	}

	// cannot publish, update apps while offline
	if payload.RequestedState == common.PUBLISHED || (payload.RequestedState == common.PRESENT && payload.RequestUpdate) {
		return true
	}

	return false
}

func (am *AppManager) EnsureLocalRequestedStates() error {
	rStates, err := am.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	for idx := range rStates {
		payload := rStates[idx]

		app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
		if err != nil {
			return err
		}

		safe.Go(func() {
			app.StateLock.Lock()
			currentAppState := app.CurrentState
			app.StateLock.Unlock()

			if !IsInvalidOfflineTransition(app, payload) && currentAppState != payload.RequestedState {
				err := am.RequestAppState(payload)
				if err != nil {
					log.Error().Err(err).Msg("Failed to ensure local requested state")
				}
			}
		})
	}

	return nil
}

func (am *AppManager) VerifyState(app *common.App) error {
	log.Printf("Verifying if app (%s, %s) is in latest state...", app.AppName, app.Stage)

	requestedStatePayload, err := am.AppStore.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		return err
	}

	log.Info().Msgf("Latest requested state (verify): %s", requestedStatePayload.RequestedState)

	app.StateLock.Lock()
	curAppState := app.CurrentState
	requestedState := app.RequestedState
	app.StateLock.Unlock()

	if curAppState == common.FAILED {
		log.Debug().Msg("App transition finished in a failed state")
		return nil
	}

	// use in memory requested state, since it's possible the database is not up to date yet if it's waiting for a database lock from other tasks
	// this requested state is updated properly on every new state request
	if curAppState != requestedState {
		log.Printf("App (%s, %s) is not in latest state (%s), transitioning to %s...", app.AppName, app.Stage, curAppState, requestedState)

		// transition again
		safe.Go(func() {
			builtOrPublishedToPresent := requestedStatePayload.RequestedState == common.PRESENT &&
				(curAppState == common.BUILT || curAppState == common.PUBLISHED)

			// we confirmed the release in the backend and can put the state to PRESENT now
			if builtOrPublishedToPresent {
				am.StateObserver.Notify(app, common.PRESENT)
				return
			}

			_ = am.RequestAppState(requestedStatePayload)
		})
	} else {
		err = am.syncPortState(requestedStatePayload, app)
		if err != nil {
			log.Error().Err(err).Msgf("failed to sync port state")
			return err
		}
	}

	return nil
}

func (am *AppManager) UpdateCurrentAppState(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	app.StateLock.Lock()

	curAppState := app.CurrentState

	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if curAppState == common.BUILT ||
		curAppState == common.PUBLISHED ||
		(curAppState == common.PRESENT && app.RequestedState == common.BUILT) {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	app.StateLock.Unlock()

	return am.AppStore.UpdateLocalAppState(app, curAppState)
}

func (am *AppManager) CreateOrUpdateApp(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	// in case requested state is transient, convert to actual state
	payload.RequestedState = common.TransientToActualState(payload.RequestedState)

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app, err = am.AppStore.AddApp(payload)
		if err != nil {
			return err
		}
	}

	app.StateLock.Lock()

	// Whenever a release confirmation gets requested, we shouldn't override the requestedState if it's not 'BUILT'
	// For example: whenever the app state goes from FAILED -> RUNNING, it will attempt building with requestedState as 'RUNNING'
	// this makes sure we don't override this requestedState to 'PRESENT'
	if app.CurrentState == common.BUILT && app.RequestedState != common.BUILT {
		app.StateLock.Unlock()
		return nil
	}

	// normally should always update the app's requestedState using the transition payload
	app.RequestedState = payload.RequestedState

	app.StateLock.Unlock()

	return am.AppStore.UpdateLocalRequestedState(payload)
}

// EnsureRemoteRequestedStates iterates over all requested states found in the local database, and transitions were neccessary.
func (am *AppManager) EnsureRemoteRequestedStates() error {
	payloads, err := am.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	for i := range payloads {
		payload := payloads[i]

		// do not execute publishes on reconnect
		if payload.RequestedState == common.PUBLISHED {
			continue
		}

		safe.Go(func() {
			am.RequestAppState(payload)
		})
	}

	return nil
}

// UpdateLocalRequestedAppStatesWithRemote is responsible for fetching any requested app states from the remote database.
// The local database will be updated with the fetched requested states. In case an app state does exist yet locally, one will be created.
func (am *AppManager) UpdateLocalRequestedAppStatesWithRemote() error {
	newestPayloads, err := am.AppStore.FetchRequestedAppStates()
	if err != nil {
		return err
	}

	log.Info().Msgf("Found %d app states, updating local database with new requested states..", len(newestPayloads))
	pretty, err := common.PrettyFormat(newestPayloads)
	if err == nil {
		log.Debug().Msgf("Payloads: %s", pretty)
	}

	for i := range newestPayloads {
		payload := newestPayloads[i]

		portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
		if err != nil {
			return err
		}

		if payload.Stage == common.PROD {
			app, err := am.AppStore.GetApp(payload.AppKey, common.PROD)
			if err != nil {
				return err
			}

			for _, portRule := range portRules {
				if !portRule.Public {
					continue
				}

				// if the app is currently running, spawn a tunnel, else just register it for later spawnage
				if app.CurrentState == common.RUNNING {
					_, err = am.AppTunnelManager.CreateAppTunnel(portRule.AppKey, portRule.DeviceKey, portRule.Port, portRule.Protocol, "")
					if err != nil {
						return err
					}
				} else {
					am.AppTunnelManager.RegisterAppTunnel(portRule.AppKey, portRule.DeviceKey, portRule.Port, portRule.Protocol, "")
				}

			}
		}

		err = am.CreateOrUpdateApp(payload)
		if err != nil {
			return err
		}
	}

	// currentAppState := payload.CurrentState
	// containerName := common.BuildContainerName(payload.Stage, payload.AppKey, payload.AppName)

	// ctx := context.Background()
	// container, err := am.StateMachine.Container.GetContainerState(ctx, containerName)
	// if err != nil {
	// 	if !errdefs.IsContainerNotFound(err) {
	// 		return err
	// 	}

	// 	// we should check if the image exists, if it does not, we should set the state to 'REMOVED', else to 'STOPPED'
	// 	var fullImageName string
	// 	if payload.Stage == common.DEV {
	// 		fullImageName = payload.RegistryImageName.Dev
	// 	} else if payload.Stage == common.PROD {
	// 		fullImageName = payload.RegistryImageName.Prod
	// 	}

	// 	images, err := am.StateMachine.Container.GetImages(ctx, fullImageName)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	var correctedAppState common.AppState
	// 	// no images were found (and no container) for this guy, so this guy is REMOVED
	// 	if len(images) == 0 {
	// 		if currentAppState == common.UNINSTALLED {
	// 			correctedAppState = common.UNINSTALLED
	// 		} else {
	// 			correctedAppState = common.REMOVED
	// 		}
	// 	} else {
	// 		// images were found for this guy, but no container, this means --> PRESENT
	// 		correctedAppState = common.PRESENT
	// 	}

	// 	if err != nil {
	// 		log.Error().Err(err).Msg("failed to notify app state")
	// 	}

	// 	payload.CurrentState = correctedAppState

	// } else {

	// 	appStateDeterminedByContainer, err := common.ContainerStateToAppState(container.Status, int(container.ExitCode))
	// 	if err != nil {
	// 		return err
	// 	}

	// 	var correctedAppState common.AppState
	// 	switch currentAppState {
	// 	case common.DOWNLOADING,
	// 		common.TRANSFERING,
	// 		common.BUILDING,
	// 		common.PUBLISHING:
	// 		correctedAppState = common.REMOVED
	// 	case common.UPDATING:
	// 		correctedAppState = common.PRESENT
	// 	case common.STOPPING,
	// 		common.STARTING:
	// 		correctedAppState = appStateDeterminedByContainer
	// 	default:
	// 		correctedAppState = appStateDeterminedByContainer
	// 	}

	// 	payload.CurrentState = correctedAppState
	// }

	return nil
}
