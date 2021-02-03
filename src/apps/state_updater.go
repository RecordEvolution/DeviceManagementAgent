package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/messenger"
	"reagent/persistence"
	"regexp"
	"strings"
)

type StateUpdater struct {
	StateStorer persistence.StateStorer
	Messenger   messenger.Messenger
	Container   container.Container
}

// UpdateLocalRequestedStates will call the remote database to update all its locally stored requested app states
func (sc *StateUpdater) UpdateLocalRequestedStates() error {
	appStateChanges, err := sc.getRemoteRequestedAppStates()

	if err != nil {
		return err
	}

	err = sc.StateStorer.BulkUpsertRequestedStateChanges(appStateChanges)
	if err != nil {
		return err
	}

	return nil
}

func (sc *StateUpdater) containerStateToAppState(containerState string, status string) (common.AppState, error) {
	switch containerState {
	case "running":
		return common.RUNNING, nil
	case "exited":
		{
			exitCode := parseExitCodeFromStatus(status)
			if exitCode == "0" {
				return common.PRESENT, nil
			}
			return common.FAILED, nil
		}
	case "paused": // state shouldn't occur
		return common.PRESENT, nil
	case "restarting":
		return common.FAILED, nil
	case "dead":
		return common.FAILED, nil
	}
	return common.FAILED, fmt.Errorf("Invalid state")
}

func parseExitCodeFromStatus(status string) string {
	statusString := regexp.MustCompile(`\((.*?)\)`).FindString(status)
	return strings.TrimRight(strings.TrimLeft(statusString, "("), ")")
}

// UpdateRemoteAppStates will evaluate all (current) app states and compare them with the (current) states stored in the local database.
// Invalid states are corrected in the local database and pushed to the remote database.
func (su *StateUpdater) UpdateRemoteAppStates() error {
	ctx := context.Background()
	containers, err := su.Container.ListContainers(ctx, nil)
	localStates, err := su.StateStorer.GetAppStates()

	if err != nil {
		return err
	}
	for _, container := range containers {
		if container.Labels["real"] != "true" {
			continue
		}

		for _, localState := range localStates {
			localStateContainerName := strings.ToLower(fmt.Sprintf("%s_%d_%s", localState.Stage, localState.AppKey, localState.AppName))

			found := false
			for _, containerName := range container.Names {
				if localStateContainerName == containerName {
					found = true
				}
			}

			if !found {
				continue
			}

			databaseAppState := localState.State
			containerAppState, err := su.containerStateToAppState(container.State, container.Status)
			if err != nil {
				return err
			}

			if databaseAppState != containerAppState {
				app := common.App{AppKey: uint64(localState.AppKey), Stage: localState.Stage}
				su.UpdateRemoteAppState(&app, containerAppState)
			}
		}
	}

	return nil
}

func (sc *StateUpdater) GetLatestRequestedStates(fetchRemote bool) ([]common.TransitionPayload, error) {
	if fetchRemote {
		err := sc.UpdateLocalRequestedStates()
		if err != nil {
			return nil, err
		}
	}

	return sc.StateStorer.GetRequestedStates()
}

func (sc *StateUpdater) UpdateAppState(app *common.App, stateToSet common.AppState) error {
	err := sc.UpdateRemoteAppState(app, stateToSet)
	fmt.Println("Set remote state to", stateToSet)
	if err != nil {
		// Fail without returning, since it's ok to miss remote app state update
		fmt.Println("Failed to update remote app state", err)
	}

	return sc.StateStorer.UpdateAppState(app, stateToSet)
}

func (sc *StateUpdater) UpdateRemoteAppState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
	config := sc.Messenger.GetConfig()
	payload := []common.Dict{{
		"app_key":                  app.AppKey,
		"device_key":               config.ReswarmConfig.DeviceKey,
		"swarm_key":                config.ReswarmConfig.SwarmKey,
		"serial_number":            config.ReswarmConfig.SerialNumber,
		"stage":                    app.Stage,
		"state":                    stateToSet,
		"device_to_app_key":        app.DeviceToAppKey,
		"requestor_account_key":    app.RequestorAccountKey,
		"request_update":           app.RequestUpdate,
		"manually_requested_state": app.ManuallyRequestedState,
		"release_build":            app.ReleaseBuild,
	}}

	_, err := sc.Messenger.Call(ctx, common.TopicSetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

// TODO: move to seperate interal api layer
func (sc *StateUpdater) getRemoteRequestedAppStates() ([]common.TransitionPayload, error) {
	ctx := context.Background()
	config := sc.Messenger.GetConfig()
	args := []common.Dict{{"device_key": config.ReswarmConfig.DeviceKey}}
	result, err := sc.Messenger.Call(ctx, common.TopicGetRequestedAppStates, args, nil, nil, nil)
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
		appName := strings.Split(deviceSyncState.ContainerName, "_")[2]

		payload := common.BuildTransitionPayload(deviceSyncState.AppKey, appName,
			uint64(deviceSyncState.RequestorAccountKey),
			common.Stage(deviceSyncState.Stage),
			common.AppState(deviceSyncState.CurrentState),
			common.AppState(deviceSyncState.ManuallyRequestedState),
			&config,
		)

		if deviceSyncState.PresentVersion != "" {
			payload.PresentVersion = deviceSyncState.PresentVersion
			payload.Version = deviceSyncState.PresentVersion
		}

		if deviceSyncState.NewestVersion != "" {
			payload.NewestVersion = deviceSyncState.NewestVersion
		}

		appPayloads = append(appPayloads, payload)
	}

	return appPayloads, nil
}

func (sc *StateUpdater) GetRegistryToken(callerID uint64) (string, error) {
	ctx := context.Background()
	args := []common.Dict{{"callerID": callerID}}
	resp, err := sc.Messenger.Call(ctx, common.TopicGetRegistryToken, args, nil, nil, nil)
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
