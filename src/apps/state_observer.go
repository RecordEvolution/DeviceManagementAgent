package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"reagent/api/common"
	"reagent/container"
	"reagent/messenger"
	"reagent/persistence"
	"strings"
)

type StateObserver struct {
	Storer    persistence.Storer
	Messenger messenger.Messenger
	Container container.Container
}

func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {
	// doublecheck if state is actually achievable and set the state in the database
	err := so.Storer.UpdateAppState(app, achievedState)
	if err != nil {
		return err
	}

	err = so.setActualAppOnDeviceState(app, achievedState)
	if err != nil {
		return err
	}

	return nil
}

func (su *StateObserver) containerStatetoCommonState(containerState string) (common.AppState, error) {
	switch containerState {
	case "running":
		return common.RUNNING, nil
	case "exited":
		return common.STOPPED, nil
	case "paused": // state shouldn't occur
		return common.PRESENT, nil
	case "restarting":
		return common.FAILED, nil
	case "dead":
		return common.FAILED, nil
	}
	return common.FAILED, fmt.Errorf("Invalid state")
}

func (su *StateObserver) UpdateLocalAppStates() error {
	ctx := context.Background()
	containers, err := su.Container.ListContainers(ctx, nil)
	localStates, err := su.Storer.GetLocalAppStates()

	if err != nil {
		return err
	}
	for _, container := range containers {
		for _, localState := range localStates {
			curContainerName := strings.ToLower(fmt.Sprintf("%s_%d_%s", localState.Stage, localState.AppKey, localState.AppName))

			containerStateKw := container["state"] // running, exited, created, paused, restarting, dead
			containerState, ok := containerStateKw.(string)
			if !ok {
				return fmt.Errorf("Invalid state payload")
			}

			containerNamesKw := container["names"]
			containerNames, ok := containerNamesKw.([]string)
			if !ok {
				return fmt.Errorf("Invalid names payload")
			}

			found := false
			for _, containerName := range containerNames {
				if curContainerName == containerName {
					found = true
				}
			}

			if !found {
				continue
			}

			dbState := localState.State
			commonState, err := su.containerStatetoCommonState(containerState)
			if err != nil {
				return err
			}

			if dbState != commonState {
				app := common.App{AppKey: localState.AppKey, Stage: localState.Stage}
				su.Storer.UpdateAppState(&app, commonState)
			}
		}
	}

	return nil
}

func (su *StateObserver) getAllAppStates() ([]common.AppStateResponse, error) {
	ctx := context.Background()
	deviceKey := su.Messenger.GetConfig().DeviceKey
	args := []common.Dict{{"device_key": deviceKey}}
	result, err := su.Messenger.Call(ctx, common.TopicGetRequestedAppStates, args, nil, nil, nil)
	if err != nil {
		return []common.AppStateResponse{}, err
	}
	byteArr, err := json.Marshal(result)
	if err != nil {
		return []common.AppStateResponse{}, err
	}

	appStateResponse := make([]common.AppStateResponse, 0)
	json.Unmarshal(byteArr, &appStateResponse)

	return appStateResponse, nil
}

func (su *StateObserver) setActualAppOnDeviceState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
	config := su.Messenger.GetConfig()
	payload := []common.Dict{{
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
