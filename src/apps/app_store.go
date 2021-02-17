package apps

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

	app.Semaphore = semaphore.NewWeighted(1)
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
		Semaphore:           semaphore.NewWeighted(1),
	}

	if payload.CurrentState == "" {
		app.CurrentState = common.REMOVED
	}

	am.apps = append(am.apps, app)

	go func() {
		// Insert the newly created app state data into the database
		_, err := am.database.UpsertAppState(app, app.CurrentState)
		if err != nil {
			log.Error().Stack().Err(err)
		}
	}()

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
	registryTokenArg := resp.Arguments[0]
	registryToken, ok := registryTokenArg.(string)

	if !ok {
		return "", fmt.Errorf("Invalid registry_token payload")
	}

	return registryToken, nil
}

// UpdateLocalAppState updates the local app database
func (am *AppStore) UpdateLocalAppState(app *common.App, stateToSet common.AppState) (common.Timestamp, error) {
	return am.database.UpsertAppState(app, stateToSet)
}

// UpdateRemoteAppState update sthe remote app database
func (am *AppStore) UpdateRemoteAppState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
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
	}}

	_, err := am.messenger.Call(ctx, topics.SetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	// The update has been sent, we know that the backend is aware now
	if app.RequestUpdate {
		app.RequestUpdate = false
	}

	return nil
}

func (am *AppStore) UpdateRequestedStatesWithRemote() error {
	appStateChanges, err := am.fetchRequestedAppStates()
	if err != nil {
		return err
	}

	err = am.database.BulkUpsertRequestedStateChanges(appStateChanges)
	if err != nil {
		return err
	}

	return nil
}

func (am *AppStore) fetchRequestedAppStates() ([]common.TransitionPayload, error) {
	ctx := context.Background()
	config := am.messenger.GetConfig()
	args := []interface{}{common.Dict{"device_key": config.ReswarmConfig.DeviceKey}}
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

		appPayloads = append(appPayloads, payload)
	}

	return appPayloads, nil
}
