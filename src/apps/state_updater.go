package apps

import (
	"context"
	"encoding/json"
	"fmt"

	"reagent/common"
	"reagent/container"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

type StateUpdater struct {
	Database  persistence.Database
	Messenger messenger.Messenger
	Container container.Container
}

// UpdateLocalRequestedStates will call the remote database to update all its locally stored requested app states
func (sc *StateUpdater) UpdateLocalRequestedStates() error {
	appStateChanges, err := sc.getRemoteRequestedAppStates()

	if err != nil {
		return err
	}

	err = sc.Database.BulkUpsertRequestedStateChanges(appStateChanges)
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
	localStates, err := su.Database.GetAppStates()

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

	return sc.Database.GetRequestedStates()
}

func (sc *StateUpdater) UpdateLocalAppState(app *common.App, stateToSet common.AppState) error {
	return sc.Database.UpsertAppState(app, stateToSet)
}

// UpdateAppState updates both the remote and local app state, if updating the remote app state fails it does not return an error.
func (sc *StateUpdater) UpdateAppState(app *common.App, stateToSet common.AppState) error {
	err := sc.UpdateRemoteAppState(app, stateToSet)
	log.Printf("Set remote state to %s for %s (%s)", stateToSet, app.AppName, app.Stage)
	if err != nil {
		// Fail without returning, since it's ok to miss remote app state update
		log.Warn().Stack().Err(err).Msg("Failed to update remote app state")
	}

	return sc.UpdateLocalAppState(app, stateToSet)
}

func (sc *StateUpdater) UpdateRemoteAppState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
	config := sc.Messenger.GetConfig()

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

	_, err := sc.Messenger.Call(ctx, topics.SetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	// The update has been sent, we know that the backend is aware now
	if app.RequestUpdate {
		app.RequestUpdate = false
	}

	return nil
}

// TODO: move to seperate interal api layer
func (sc *StateUpdater) getRemoteRequestedAppStates() ([]common.TransitionPayload, error) {
	ctx := context.Background()
	config := sc.Messenger.GetConfig()
	args := []interface{}{common.Dict{"device_key": config.ReswarmConfig.DeviceKey}}
	result, err := sc.Messenger.Call(ctx, topics.GetRequestedAppStates, args, nil, nil, nil)
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

func (sc *StateUpdater) GetRegistryToken(callerID uint64) (string, error) {
	ctx := context.Background()
	args := []interface{}{common.Dict{"callerID": callerID}}
	resp, err := sc.Messenger.Call(ctx, topics.GetRegistryToken, args, nil, nil, nil)
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
