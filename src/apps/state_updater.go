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

func (su *StateUpdater) DeviceSync(fetchRemote bool) error {
	if fetchRemote {
		err := su.UpdateLocalRequestedStates()
		if err != nil {
			return err
		}
	}

	payloads, err := su.StateStorer.GetLocalRequestedStates()
	if err != nil {
		return err
	}

	for _, payload := range payloads {
		token, err := su.getRegistryToken(payload.RequestorAccountKey)
		if err != nil {
			return err
		}

		payload.RegisteryToken = token
		err = su.StateMachine.RequestAppState(payload)
		if err != nil {
			return err
		}
	}

	return nil
}

// UpdateLocalRequestedStates will call the remote database to update all its locally stored requested app states
func (su *StateUpdater) UpdateLocalRequestedStates() error {
	appStateChanges, err := su.getRemoteRequestedAppStates()

	if err != nil {
		return err
	}

	err = su.StateStorer.BulkUpsertRequestedStateChanges(appStateChanges)
	if err != nil {
		return err
	}

	return nil
}

// UpdateRemoteAppStates will evaluate all (current) app states and compare them with the (current) states stored in the local database.
// Invalid states are corrected in the local database and pushed to the remote database.
func (su *StateUpdater) UpdateRemoteAppStates() error {
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

func (su *StateUpdater) getRegistryToken(callerID int) (string, error) {
	ctx := context.Background()
	args := []common.Dict{{"callerID": callerID}}
	resp, err := su.Messenger.Call(ctx, common.TopicGetRegistryToken, args, nil, nil, nil)
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

// TODO: move to seperate interal api layer
func (su *StateUpdater) getRemoteRequestedAppStates() ([]common.TransitionPayload, error) {
	ctx := context.Background()
	config := su.Messenger.GetConfig()
	args := []common.Dict{{"device_key": config.ReswarmConfig.DeviceKey}}
	result, err := su.Messenger.Call(ctx, common.TopicGetRequestedAppStates, args, nil, nil, nil)
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
		imageName := strings.ToLower(fmt.Sprintf("%s_%s_%d_%s", deviceSyncState.Stage, config.ReswarmConfig.Architecture, deviceSyncState.AppKey, appName))
		presentImageName := strings.ToLower(fmt.Sprintf("%s%s%s", config.ReswarmConfig.DockerRegistryURL, config.ReswarmConfig.DockerMainRepository, deviceSyncState.PresentImageName))
		//repositoryImageName := strings.ToLower(fmt.Sprintf("%s%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName))

		payload := common.TransitionPayload{
			AppName:             appName,
			AppKey:              deviceSyncState.AppKey,
			ContainerName:       deviceSyncState.ContainerName,
			ImageName:           imageName,
			RepositoryImageName: presentImageName,
			RequestorAccountKey: deviceSyncState.RequestorAccountKey,
			RequestedState:      common.AppState(deviceSyncState.ManuallyRequestedState),
			CurrentState:        common.AppState(deviceSyncState.CurrentState),
			Stage:               common.Stage(deviceSyncState.Stage),
			RequestUpdate:       deviceSyncState.RequestUpdate,
		}

		appPayloads = append(appPayloads, payload)
	}

	return appPayloads, nil
}
