package apps

import (
	"fmt"
	"reagent/common"
	"reagent/errdefs"

	"github.com/rs/zerolog/log"
)

type AppManager struct {
	AppStore      *AppStore
	StateMachine  *StateMachine
	StateObserver *StateObserver
}

func NewAppManager(sm *StateMachine, as *AppStore, so *StateObserver) AppManager {
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

	if app.CurrentState == payload.RequestedState && !payload.RequestUpdate {
		log.Debug().Msgf("app %s (%s) is already on latest state (%s)", app.AppName, app.Stage, payload.RequestedState)
		return nil
	}

	// If appState is already up to date we should do nothing
	// transition cancellation will ignore the app lock
	if app.IsCancelable() {
		if payload.RequestedState == common.REMOVED && app.CurrentState == common.BUILDING {
			am.StateMachine.CancelTransition(app, payload)
			return nil
		}
	}

	locked := app.SecureLock() // if the app is not locked, it will lock the app
	if locked {
		log.Warn().Msgf("App with name %s and stage %s is already transitioning", app.AppName, app.Stage)
		return nil
	}

	// need to call this after we have secured the lock
	// to not change the actual state in the middle of an ongoing transition
	// this is necessary because some state transitions require a change of actual state (BUILD & PUBLISH)
	err = am.UpdateCurrentAppState(payload)
	if err != nil {
		app.Unlock()
		return err
	}

	// before we transition, should request the token
	token, err := am.AppStore.GetRegistryToken(payload.RequestorAccountKey)
	if err != nil {
		app.Unlock()
		return err
	}
	payload.RegisteryToken = token

	errC := am.StateMachine.PerformTransition(app, payload)
	if errC == nil {
		// not yet implemented or nullified state transition
		app.Unlock()
		return nil
	}

	// block till transition has finished
	select {
	case err := <-errC:
		app.Unlock()

		if errdefs.IsNoActionTransition(err) {
			log.Info().Msg("A no action transition was executed, nothing to do.")
			return nil
		}

		if err == nil {
			log.Info().Msgf("Successfully finished transaction for App (%d, %s)", app.AppKey, app.Stage)

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
				err = fmt.Errorf("Failed to complete transition: %w; Failed to set state to 'FAILED';", err)
			}
		}

		log.Error().Msgf("An error occured during transition from %s to %s", app.CurrentState, payload.RequestedState)
		log.Error().Err(err).Msg("The current app state will has been set to FAILED")
	}

	return nil
}

func (am *AppManager) VerifyState(app *common.App) error {
	log.Printf("Verifying if app (%d, %s) is in latest state...", app.AppKey, app.Stage)

	requestedStatePayload, err := am.AppStore.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		return err
	}

	log.Info().Msgf("Latest requested state (verify): %s", requestedStatePayload.RequestedState)

	// TODO: what to do when the app transition fails? How do we handle that?
	if app.CurrentState == common.FAILED {
		log.Print("App transition finished in a failed state")
		return nil
	}

	if requestedStatePayload.RequestedState != app.CurrentState {
		log.Printf("App (%d, %s) is not in latest state (%s), transitioning to %s...", app.AppKey, app.Stage, app.CurrentState, requestedStatePayload.RequestedState)

		// transition again
		go func() {
			builtOrPublishedToPresent := requestedStatePayload.RequestedState == common.PRESENT &&
				(app.CurrentState == common.BUILT || app.CurrentState == common.PUBLISHED)

			// we confirmed the release in the backend and can put the state to PRESENT now
			if builtOrPublishedToPresent {
				app.CurrentState = common.PRESENT // also set in memory
				_, err := am.AppStore.UpdateLocalAppState(app, common.PRESENT)
				if err != nil {
					log.Error().Stack().Err(err)
				}

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

	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if app.CurrentState == common.BUILT || app.CurrentState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	go func() {
		timestamp, err := am.AppStore.UpdateLocalAppState(app, app.CurrentState)
		if err != nil {
			log.Error().Err(err)
		}

		app.LastUpdated = timestamp
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

	// Whenever a release confirmation gets requested, we shouldn't override the requestedState if it's not 'BUILT'
	// For example: whenever the app state goes from FAILED -> RUNNING, it will attempt building with requestedState as 'RUNNING'
	// this makes sure we don't override this requestedState to 'PRESENT'
	if app.CurrentState == common.BUILT && app.RequestedState != common.BUILT {
		return nil
	}

	// normally should always update the app's requestedState using the transition payload
	app.RequestedState = payload.RequestedState

	go func() {
		err = am.AppStore.UpdateLocalRequestedState(payload)
		if err != nil {
			log.Error().Stack().Err(err)
		}
	}()

	return nil
}

func (am *AppManager) Sync() error {
	log.Info().Msg("Device Sync Initialized...")

	// first update the requestedStates that we have in the database
	err := am.AppStore.UpdateRequestedStatesWithRemote()
	if err != nil {
		return err
	}

	payloads, err := am.AppStore.GetRequestedStates()
	if err != nil {
		return err
	}

	log.Info().Msgf("Checking current app states for %d apps..", len(payloads))

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

		// transition the app, if changed, to the latest state
		go am.RequestAppState(payload)
	}

	return nil
}
