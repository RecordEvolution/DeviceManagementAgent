package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"reagent/api/common"
	"reagent/messenger"
	"reagent/persistence"
	"regexp"
	"strings"
)

type StateUpdater struct {
	StateMachine StateMachine
	StateStorer  persistence.StateStorer
	Messenger    messenger.Messenger
}

func (su *StateUpdater) containerStateToAppState(containerState string, status string) (common.AppState, error) {
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

// ExecuteStateChangeUpdatesFromRemoteDatabase executes the manually requested states from the database
func (su *StateUpdater) ExecuteStateChangeUpdatesFromRemoteDatabase() error {
	appStates, err := su.getAllAppStates()
	config := su.Messenger.GetConfig()

	if err != nil {
		return err
	}

	for _, appState := range appStates {
		payload := AppStateToTransitionPayload(config, appState)
		su.StateMachine.RequestAppState(payload)
	}

	return nil
}

// UpdateCurrentAppStatesToRemoteDb will evaluate all local container states and compare them with the states stored in the local database.
// Invalid states are corrected in the local database and pushed to the remote database.
func (su *StateUpdater) UpdateCurrentAppStatesToRemoteDb() error {
	ctx := context.Background()
	containers, err := su.StateMachine.Container.ListContainers(ctx, nil)
	localStates, err := su.StateStorer.GetLocalAppStates()

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
				su.StateStorer.UpdateAppState(&app, containerAppState)
			}
		}
	}

	return nil
}

// TODO: move to seperate interal api layer
func (su *StateUpdater) getAllAppStates() ([]common.App, error) {
	ctx := context.Background()
	deviceKey := su.Messenger.GetConfig().DeviceKey
	args := []common.Dict{{"device_key": deviceKey}}
	result, err := su.Messenger.Call(ctx, common.TopicGetRequestedAppStates, args, nil, nil, nil)
	if err != nil {
		return []common.App{}, err
	}
	byteArr, err := json.Marshal(result.Arguments[0])
	if err != nil {
		return []common.App{}, err
	}

	deviceSyncStateResponse := make([]common.DeviceSyncResponse, 0)
	json.Unmarshal(byteArr, &deviceSyncStateResponse)

	appStates := make([]common.App, 0)
	for _, deviceSyncState := range deviceSyncStateResponse {
		app := common.App{
			Name:                   deviceSyncState.Name,
			AppKey:                 deviceSyncState.AppKey,
			ManuallyRequestedState: common.AppState(deviceSyncState.ManuallyRequestedState),
			CurrentState:           common.AppState(deviceSyncState.CurrentState),
			Stage:                  common.Stage(deviceSyncState.Stage),
			RequestUpdate:          deviceSyncState.RequestUpdate,
		}

		appStates = append(appStates, app)
	}

	return appStates, nil
}
