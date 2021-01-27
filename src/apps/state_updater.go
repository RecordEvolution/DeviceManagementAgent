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

func (su *StateUpdater) UpdateLocalAppStates() error {
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
				app := common.App{AppKey: localState.AppKey, Stage: localState.Stage}
				su.StateStorer.UpdateAppState(&app, containerAppState)
			}
		}
	}

	return nil
}

func (su *StateUpdater) getAllAppStates() ([]common.AppStateResponse, error) {
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
