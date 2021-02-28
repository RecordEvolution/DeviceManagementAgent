package apps

import (
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"reagent/safe"
	"reagent/store"

	"github.com/rs/zerolog/log"
)

type AppManager struct {
	AppStore      *store.AppStore
	StateMachine  *StateMachine
	StateObserver *StateObserver
}

func NewAppManager(sm *StateMachine, as *store.AppStore, so *StateObserver) AppManager {
	return AppManager{
		StateMachine:  sm,
		StateObserver: so,
		AppStore:      as,
	}
}

func (am *AppManager) RequestAppState(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	if curAppState == payload.RequestedState && !payload.RequestUpdate {
		log.Debug().Msgf("App Manager: app %s (%s) is already on latest state (%s)", app.AppName, app.Stage, payload.RequestedState)
		return nil
	}

	if payload.CancelTransition {
		log.Debug().Msgf("App Manager: Cancel request was received for %s (%s) (currently: %s)", app.AppName, app.Stage, app.CurrentState)
		am.StateMachine.CancelTransition(app, payload)
		return nil
	}

	locked := app.SecureTransition() // if the app is not locked, it will lock the app
	if locked {
		log.Warn().Msgf("App Manager: App with name %s and stage %s is already transitioning", app.AppName, app.Stage)
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

	errC := am.StateMachine.PerformTransition(app, payload)
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
			log.Debug().Msg("App Manager: A no action transition was executed, nothing to do. Will also not verify")
			return nil
		}

		isCanceled := errdefs.IsDockerStreamCanceled(err)
		if err == nil || isCanceled {
			if !isCanceled {
				log.Info().Msgf("App Manager: Successfully finished transaction for App (%s, %s)", app.AppName, app.Stage)
			} else {
				log.Info().Msgf("App Manager: Successfully canceled transition for App (%s, %s)", app.AppName, app.Stage)
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
				err = fmt.Errorf("App Manager: Failed to complete transition: %w; Failed to set state to 'FAILED';", err)
			}
		}

		log.Error().Msgf("App Manager: An error occured during transition from %s to %s for %s (%s)", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
		log.Error().Err(err).Msg("App Manager: The current app state will has been set to FAILED")
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

	// cannot publish apps while offline
	if payload.RequestedState == common.PUBLISHED {
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
			if !IsInvalidOfflineTransition(app, payload) && payload.RequestedState != app.CurrentState {
				err := am.RequestAppState(payload)
				if err != nil {
					log.Error().Err(err)
				}
			}
		})
	}

	return nil
}

func (am *AppManager) VerifyState(app *common.App) error {
	log.Printf("App Manager: Verifying if app (%s, %s) is in latest state...", app.AppName, app.Stage)

	requestedStatePayload, err := am.AppStore.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		return err
	}

	log.Info().Msgf("App Manager: Latest requested state (verify): %s", requestedStatePayload.RequestedState)

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	if curAppState == common.FAILED {
		log.Debug().Msg("App Manager: App transition finished in a failed state")
		return nil
	}

	// use in memory requested state, since it's possible the database is not up to date yet if it's waiting for a database lock from other tasks
	// this requested state is updated properly on every new state request
	if app.RequestedState != curAppState {
		log.Printf("App Manager: App (%s, %s) is not in latest state (%s), transitioning to %s...", app.AppName, app.Stage, curAppState, requestedStatePayload.RequestedState)

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
	}

	return nil
}

func TempUpdateCurrentAppState(appStore *store.AppStore, payload common.TransitionPayload) error {
	app, err := appStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	app.StateLock.Lock()

	curAppState := app.CurrentState

	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if curAppState == common.BUILT || curAppState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	app.StateLock.Unlock()

	safe.Go(func() {
		app.StateLock.Lock()
		curAppState := app.CurrentState
		app.StateLock.Unlock()

		timestamp, err := appStore.UpdateLocalAppState(app, curAppState)
		if err != nil {
			log.Error().Err(err)
		}

		app.StateLock.Lock()
		app.LastUpdated = timestamp
		app.StateLock.Unlock()
	})

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
	if curAppState == common.BUILT || curAppState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	app.StateLock.Unlock()

	safe.Go(func() {
		app.StateLock.Lock()
		curAppState := app.CurrentState
		app.StateLock.Unlock()

		timestamp, err := am.AppStore.UpdateLocalAppState(app, curAppState)
		if err != nil {
			log.Error().Err(err)
		}

		app.StateLock.Lock()
		app.LastUpdated = timestamp
		app.StateLock.Unlock()
	})

	return nil
}

func (am *AppManager) CreateOrUpdateApp(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		return err
	}

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app, err = am.AppStore.AddApp(payload)
		if err != nil {
			return err
		}
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	// Whenever a release confirmation gets requested, we shouldn't override the requestedState if it's not 'BUILT'
	// For example: whenever the app state goes from FAILED -> RUNNING, it will attempt building with requestedState as 'RUNNING'
	// this makes sure we don't override this requestedState to 'PRESENT'
	if curAppState == common.BUILT && app.RequestedState != common.BUILT {
		return nil
	}

	// normally should always update the app's requestedState using the transition payload
	app.StateLock.Lock()
	app.RequestedState = payload.RequestedState
	app.StateLock.Unlock()

	safe.Go(func() {
		err = am.AppStore.UpdateLocalRequestedState(payload)
		if err != nil {
			log.Error().Stack().Err(err)
		}
	})

	return nil
}

// EvaluateRequestedStates iterates over all requested states found in the local database, and transitions were neccessary.
func (am *AppManager) EvaluateRequestedStates() error {
	payloads, err := am.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	for i := range payloads {
		payload := payloads[i]
		safe.Go(func() {
			am.RequestAppState(payload)
		})
	}

	return nil
}

// UpdateLocalRequestedAppStatesWithRemote is responsible for fetching any requested app states from the remote database.
// The local database will be updated with the fetched requested states. In case an app state does exist yet locally, one will be created.
func (am *AppManager) UpdateLocalRequestedAppStatesWithRemote() error {
	// first update the requestedStates that we have in the database
	err := am.AppStore.UpdateRequestedStatesWithRemote()
	if err != nil {
		return err
	}

	payloads, err := am.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	log.Info().Msgf("App Manager: Found %d app states, updating local database with new requested states..", len(payloads))

	for i := range payloads {
		payload := payloads[i]

		app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
		if err != nil {
			return err
		}

		// if the app doesn't exist yet, make sure it gets added to the local db / memory
		if app == nil {
			_, err = am.AppStore.AddApp(payload)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
