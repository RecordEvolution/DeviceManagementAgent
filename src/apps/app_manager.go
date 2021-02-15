package apps

import (
	"reagent/common"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type AppManager struct {
	StateMachine  *StateMachine
	StateUpdater  *StateUpdater
	StateObserver *StateObserver
	apps          []*common.App
}

func NewAppManager(sm *StateMachine, su *StateUpdater) AppManager {
	apps := make([]*common.App, 0)
	return AppManager{
		StateMachine: sm,
		StateUpdater: su,
		apps:         apps,
	}
}

func (am *AppManager) RequestAppState(app *common.App, payload common.TransitionPayload) error {
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

	// unlock the app on function exit
	defer app.Unlock()

	// need to call this after we have secured the lock
	// to not change the actual state in the middle of an ongoing transition
	// this is necessary because some state transitions require a change of actual state (BUILD & PUBLISH)
	err := am.UpdateCurrentAppStateWithPayload(app, payload)
	if err != nil {
		return err
	}

	// before we transition, should request the token
	token, err := am.StateUpdater.GetRegistryToken(payload.RequestorAccountKey)
	if err != nil {
		return err
	}

	payload.RegisteryToken = token

	errC := am.StateMachine.PerformTransition(app, payload)

	// block till transition has finished
	select {
	case err := <-errC:
		if err == nil {
			log.Info().Msgf("Successfully finished transaction")
			// Verify if app has the latest requested state
			// TODO: properly handle it when verifying fails
			err := am.VerifyState(app)
			if err != nil {
				log.Error().Err(err).Msgf("failed to verify app state")
				return err
			}
			return nil
		}

		log.Error().Msgf("An error occured during transition from %s to %s", app.CurrentState, payload.RequestedState)
		log.Error().Err(err).Msg("The current app state will has been set to FAILED")
	}

	return nil
}

func (am *AppManager) VerifyState(app *common.App) error {
	log.Printf("Verifying if app (%d, %s) is in latest state...", app.AppKey, app.Stage)

	requestedStatePayload, err := am.StateUpdater.StateStorer.GetRequestedState(app)
	if err != nil {
		return err
	}

	log.Warn().Msgf("requested state according to verify: %s", requestedStatePayload.RequestedState)

	// TODO: what to do when the app transition fails? How do we handle that?
	if app.CurrentState == common.FAILED {
		log.Print("App transition finished in a failed state")
		return nil
	}

	defer log.Printf("App (%d, %s) is in latest state!", app.AppKey, app.Stage)

	if requestedStatePayload.RequestedState != app.CurrentState {
		log.Printf("App (%d, %s) is not in latest state (%s), transitioning to %s...", app.AppKey, app.Stage, app.CurrentState, requestedStatePayload.RequestedState)

		builtOrPublishedToPresent := requestedStatePayload.RequestedState == common.PRESENT &&
			(app.CurrentState == common.BUILT || app.CurrentState == common.PUBLISHED)

		// we confirmed the release in the backend and can put the state to PRESENT now
		if builtOrPublishedToPresent {
			app.CurrentState = common.PRESENT // also set in memory
			am.StateUpdater.StateStorer.UpsertAppState(app, common.PRESENT)
			return nil
		}

		go am.RequestAppState(app, requestedStatePayload)
	}

	return nil
}

func (am *AppManager) UpdateCurrentAppStateWithPayload(app *common.App, payload common.TransitionPayload) error {
	// Building and Publishing actions will set the state to 'REMOVED' temporarily to perform a build
	if app.CurrentState == common.BUILT || app.CurrentState == common.PUBLISHED {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	err := am.StateUpdater.StateStorer.UpsertAppState(app, app.CurrentState)
	if err != nil {
		return err
	}

	return nil
}

func (am *AppManager) CreateOrUpdateApp(payload common.TransitionPayload) (*common.App, error) {
	app := am.getApp(payload.AppKey, payload.Stage)

	// if app was not found in memory, will create a new entry from payload
	if app == nil {
		app = &common.App{
			AppKey:              payload.AppKey,
			AppName:             payload.AppName,
			CurrentState:        payload.CurrentState,
			DeviceToAppKey:      payload.DeviceToAppKey,
			ReleaseKey:          payload.ReleaseKey,
			RequestorAccountKey: payload.RequestorAccountKey,
			RequestedState:      payload.RequestedState,
			Stage:               payload.Stage,
			Version:             payload.PresentVersion,
			RequestUpdate:       payload.RequestUpdate,
			Semaphore:           semaphore.NewWeighted(1),
		}

		if payload.CurrentState == "" {
			app.CurrentState = common.REMOVED
		}

		// Insert the newly created app state data into the database
		err := am.StateUpdater.StateStorer.UpsertAppState(app, app.CurrentState)
		if err != nil {
			return nil, err
		}

		am.apps = append(am.apps, app)
	}

	// always update the  app's requestedState using the transition payload
	app.RequestedState = payload.RequestedState
	err := am.StateUpdater.StateStorer.UpsertRequestedStateChange(payload)
	if err != nil {
		return nil, err
	}

	return app, nil
}

func (am *AppManager) Sync() error {
	log.Info().Msg("Device Sync Initialized...")

	payloads, err := am.StateUpdater.GetLatestRequestedStates(true)
	if err != nil {
		return err
	}

	log.Info().Msgf("Checking current app states for %d apps..", len(payloads))

	for i := range payloads {
		payload := payloads[i]

		// if the app doesn't exist yet, make sure it gets added to the local db
		// also populates app manager with the current states
		app, err := am.CreateOrUpdateApp(payload)
		if err != nil {
			return err
		}

		// transition the app, if changed, to the latest state
		go am.RequestAppState(app, payload)
	}

	return nil
}

func (am *AppManager) getApp(appKey uint64, stage common.Stage) *common.App {
	for i := range am.apps {
		state := am.apps[i]
		if state.AppKey == appKey && state.Stage == stage {
			return state
		}
	}
	return nil
}
