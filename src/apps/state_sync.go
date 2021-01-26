package apps

import (
	"context"
	"reagent/api/common"
	"reagent/messenger"
)

type StateSyncer struct {
	Messenger messenger.Messenger
}

func (su *StateSyncer) Sync(app *common.App, stateToSet common.AppState) error {
	return su.setActualAppOnDeviceState(app, stateToSet)
}

func (su *StateSyncer) setActualAppOnDeviceState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
	config := su.Messenger.GetConfig()
	payload := []messenger.Dict{{
		"app_key":                  app.AppKey,
		"device_key":               config.DeviceKey,
		"swarm_key":                config.SwarmKey,
		"stage":                    app.Stage,
		"state":                    stateToSet,
		"request_update":           app.RequestUpdate,
		"manually_requested_state": app.ManuallyRequestedState,
	}}

	// See containers.ts
	if stateToSet == common.BUILDING {
		payload[0]["version"] = "latest"
	}

	// args := []messenger.Dict{payload}

	_, err := su.Messenger.Call(ctx, common.TopicSetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}
