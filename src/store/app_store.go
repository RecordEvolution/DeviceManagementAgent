package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type AppStore struct {
	database  persistence.Database
	messenger messenger.Messenger
	apps      []*common.App
}

func NewAppStore(database persistence.Database, messenger messenger.Messenger) AppStore {
	return AppStore{
		database:  database,
		messenger: messenger,
		apps:      make([]*common.App, 0),
	}
}

func (am *AppStore) SetMessenger(messenger messenger.Messenger) {
	am.messenger = messenger
}

func (am *AppStore) GetAllApps() ([]*common.App, error) {
	return am.database.GetAppStates()
}

func (am *AppStore) GetRequestedStates() ([]common.TransitionPayload, error) {
	return am.database.GetRequestedStates()
}

func (am *AppStore) GetRequestedState(appKey uint64, stage common.Stage) (common.TransitionPayload, error) {
	return am.database.GetRequestedState(appKey, stage)
}

func (am *AppStore) GetApp(appKey uint64, stage common.Stage) (*common.App, error) {
	for i := range am.apps {
		state := am.apps[i]
		if state.AppKey == appKey && state.Stage == stage {
			return state, nil
		}
	}

	// app was not found in memory
	app, err := am.database.GetAppState(appKey, stage)
	if err != nil {
		return nil, err
	}

	// app was also not found in database
	if app == nil {
		return nil, nil
	}

	app.TransitionLock = semaphore.NewWeighted(1)
	am.apps = append(am.apps, app)

	return app, nil
}

func (am *AppStore) AddApp(payload common.TransitionPayload) (*common.App, error) {
	app := &common.App{
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
		TransitionLock:      semaphore.NewWeighted(1),
	}

	if payload.CurrentState == "" {
		app.StateLock.Lock()
		app.CurrentState = common.REMOVED
		app.StateLock.Unlock()
	}

	am.apps = append(am.apps, app)

	// Insert the newly created app state data into the database
	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	_, err := am.database.UpsertAppState(app, curAppState)
	if err != nil {
		log.Error().Stack().Err(err)
	}

	if err != nil {
		return nil, err
	}

	return app, nil
}

func (am *AppStore) UpdateLocalRequestedState(payload common.TransitionPayload) error {
	return am.database.UpsertRequestedStateChange(payload)
}

func (am *AppStore) GetRegistryToken(callerID uint64) (string, error) {
	ctx := context.Background()
	args := []interface{}{common.Dict{"callerID": callerID}}
	resp, err := am.messenger.Call(ctx, topics.GetRegistryToken, args, nil, nil, nil)
	if err != nil {
		return "", err
	}

	if resp.Arguments == nil {
		return "", nil
	}

	registryTokenArg := resp.Arguments[0]
	registryToken, ok := registryTokenArg.(string)

	if !ok {
		return "", fmt.Errorf("invalid registry_token payload")
	}

	return registryToken, nil
}

// UpdateLocalAppState updates the local app database
func (am *AppStore) UpdateLocalAppState(app *common.App, stateToSet common.AppState) error {
	timestamp, err := am.database.UpsertAppState(app, stateToSet)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upsert app state")
	}

	app.StateLock.Lock()
	app.LastUpdated = timestamp
	app.StateLock.Unlock()

	if err != nil {
		return err
	}

	return nil
}

// UpdateRemoteAppState update sthe remote app database
func (am *AppStore) UpdateRemoteAppState(ctx context.Context, app *common.App, stateToSet common.AppState) error {
	config := am.messenger.GetConfig()

	payload := []interface{}{common.Dict{
		"app_key":               app.AppKey,
		"device_key":            config.ReswarmConfig.DeviceKey,
		"swarm_key":             config.ReswarmConfig.SwarmKey,
		"serial_number":         config.ReswarmConfig.SerialNumber,
		"stage":                 app.Stage,
		"state":                 stateToSet,
		"device_to_app_key":     app.DeviceToAppKey,
		"requestor_account_key": app.RequestorAccountKey,
		"release_key":           app.ReleaseKey,
		"request_update":        app.RequestUpdate,
		"release_build":         app.ReleaseBuild,
		"updateStatus":          app.UpdateStatus,
	}}

	_, err := am.messenger.Call(ctx, topics.SetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	app.StateLock.Lock()
	// we successfully let the backend know we updated, we can now set this to false
	if app.UpdateStatus == common.PENDING_REMOTE_CONFIRMATION {
		app.UpdateStatus = common.COMPLETED
	}

	app.StateLock.Unlock()

	return nil
}

func (am *AppStore) UpdateRequestedStatesWithRemote() error {
	appStateChanges, err := am.FetchRequestedAppStates()
	if err != nil {
		return err
	}

	err = am.database.BulkUpsertRequestedStateChanges(appStateChanges)
	if err != nil {
		return err
	}

	return nil
}

func (am *AppStore) FetchRequestedAppStates() ([]common.TransitionPayload, error) {
	ctx := context.Background()
	config := am.messenger.GetConfig()
	args := []interface{}{common.Dict{"device_key": config.ReswarmConfig.DeviceKey}}

	log.Debug().Msgf("Fetching requested states for device with key: %d", config.ReswarmConfig.DeviceKey)

	result, err := am.messenger.Call(ctx, topics.GetRequestedAppStates, args, nil, nil, nil)
	if err != nil {
		return []common.TransitionPayload{}, err
	}
	byteArr, err := json.Marshal(result.Arguments[0])
	if err != nil {
		return []common.TransitionPayload{}, err
	}

	deviceSyncStateResponse := make([]common.DeviceSyncResponse, 0)
	json.Unmarshal(byteArr, &deviceSyncStateResponse)

	appPayloads := make([]common.TransitionPayload, 0)
	for _, deviceSyncState := range deviceSyncStateResponse {

		payload := common.BuildTransitionPayload(deviceSyncState.AppKey, deviceSyncState.AppName,
			uint64(deviceSyncState.RequestorAccountKey),
			common.Stage(deviceSyncState.Stage),
			common.AppState(deviceSyncState.CurrentState),
			common.AppState(deviceSyncState.TargetState),
			uint64(deviceSyncState.ReleaseKey),
			uint64(deviceSyncState.NewReleaseKey),
			config,
		)

		payload.RequestUpdate = deviceSyncState.RequestUpdate
		payload.PresentVersion = deviceSyncState.PresentVersion
		payload.Version = deviceSyncState.PresentVersion
		payload.NewestVersion = deviceSyncState.NewestVersion
		payload.EnvironmentVariables = deviceSyncState.Environment
		payload.EnvironmentTemplate = deviceSyncState.EnvironmentTemplate
		payload.Ports = deviceSyncState.Ports

		appPayloads = append(appPayloads, payload)
	}

	return appPayloads, nil
}
