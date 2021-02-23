package apps

import (
	"fmt"
	"reagent/common"
	"reagent/errdefs"
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

	// If appState is already up to date we should do nothing
	// transition cancellation will ignore the app lock
	if app.IsCancelable() {
		if payload.RequestedState == common.REMOVED && curAppState == common.BUILDING {
			am.StateMachine.CancelTransition(app, payload)
			return nil
		}
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

		if err == nil {
			log.Info().Msgf("App Manager: Successfully finished transaction for App (%s, %s)", app.AppName, app.Stage)

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

	if requestedStatePayload.RequestedState != curAppState {
		log.Printf("App Manager: App (%s, %s) is not in latest state (%s), transitioning to %s...", app.AppName, app.Stage, curAppState, requestedStatePayload.RequestedState)

		// transition again
		go func() {
			builtOrPublishedToPresent := requestedStatePayload.RequestedState == common.PRESENT &&
				(curAppState == common.BUILT || curAppState == common.PUBLISHED)

			// we confirmed the release in the backend and can put the state to PRESENT now
			if builtOrPublishedToPresent {
				am.StateObserver.Notify(app, common.PRESENT)
				return
			}

			_ = am.RequestAppState(requestedStatePayload)
		}()
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
	app.StateLock.Unlock()

	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if curAppState == common.BUILT || curAppState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.StateLock.Lock()
			app.CurrentState = payload.CurrentState
			app.StateLock.Unlock()
		}
	}

	if payload.PresentVersion != "" {
		app.StateLock.Lock()
		app.Version = payload.PresentVersion
		app.StateLock.Unlock()
	}

	go func() {
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
	}()

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

	go func() {
		err = am.AppStore.UpdateLocalRequestedState(payload)
		if err != nil {
			log.Error().Stack().Err(err)
		}
	}()

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
		go am.RequestAppState(payload)
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
