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

// UpdateAppStates will evaluate all local container states and compare that with states stored in the database.
// Invalid states are corrected in the local and remote database.
func (su *StateUpdater) UpdateAppStates() error {
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

func (su *StateUpdater) LoadRemoteAppStates() error {
	remoteAppStates, err := su.getAllAppStates()
	if err != nil {
		return nil
	}

	su.StateMachine.PopulateState(remoteAppStates)
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
	byteArr, err := json.Marshal(result)
	if err != nil {
		return []common.App{}, err
	}

	appStateResponse := make([]common.AppStateResponse, 0)
	json.Unmarshal(byteArr, &appStateResponse)

	appStates := make([]common.App, 0)
	for _, appState := range appStateResponse {
		app := common.App{
			Name:                   appState.Name,
			AppKey:                 appState.AppKey,
			AppName:                appState.AppName,
			ManuallyRequestedState: common.AppState(appState.ManuallyRequestedState),
			CurrentState:           common.AppState(appState.CurrentState),
			Stage:                  common.Stage(appState.Stage),
		}

		requestUpdateKw := appState.RequestUpdate
		if requestUpdate, ok := requestUpdateKw.(bool); ok {
			app.RequestUpdate = requestUpdate
		}

		appStates = append(appStates, app)
	}

	return appStates, nil
}
