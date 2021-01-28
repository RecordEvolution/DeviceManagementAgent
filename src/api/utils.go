package api

import (
	"fmt"
	"reagent/api/common"
	"reagent/config"
	"reagent/messenger"
	"strings"
)

// ResponseTocommon.TransitionPayload parses a Messenger response to a generic common.TransitionPayload struct.
// Values that were not provided will be nil.
func ResponseToTransitionPayload(config config.Config, result messenger.Result) (common.TransitionPayload, error) {
	kwargs := result.ArgumentsKw
	details := result.Details

	appKeyKw := kwargs["app_key"]
	appNameKw := kwargs["app_name"]
	stageKw := kwargs["stage"]
	requestedStateKw := kwargs["manually_requested_state"]
	currentStateKw := kwargs["current_state"]
	registryTokenKw := kwargs["registry_token"]

	var appKey uint64
	var appName string
	var stage string
	var requestedState string
	var currentState string
	var registryToken string

	var ok bool

	// TODO: can be simplified with parser function, but unneccessary
	if appKeyKw != nil {
		appKey, ok = appKeyKw.(uint64)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse app_key")
		}
	}

	if appNameKw != nil {
		appName, ok = appNameKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse appName")
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse stage")
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse requested_state")
		}
	}

	if currentStateKw != nil {
		currentState, ok = currentStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse currentState")
		}
	}

	if registryTokenKw != nil {
		registryToken, ok = registryTokenKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse registryToken")
		}
	}

	callerAuthIDString := details["caller_authid"]

	// callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))

	callerAuthID, ok := callerAuthIDString.(string)
	if !ok {
		return common.TransitionPayload{}, fmt.Errorf("Failed to parse callerAuthid")
	}

	containerName := fmt.Sprintf("%s_%d_%s", stage, appKey, appName)
	imageName := strings.ToLower(fmt.Sprintf("%s_%s_%d_%s", stage, config.ReswarmConfig.Architecture, appKey, appName))
	fullImageName := strings.ToLower(fmt.Sprintf("%s%s%s", config.ReswarmConfig.DockerRegistryURL, config.ReswarmConfig.DockerMainRepository, imageName))

	return common.TransitionPayload{
		Stage:               common.Stage(stage),
		RequestedState:      common.AppState(requestedState),
		AppName:             appName,
		AppKey:              appKey,
		CurrentState:        common.AppState(currentState),
		ContainerName:       strings.ToLower(containerName),
		ImageName:           imageName,
		RepositoryImageName: strings.ToLower(fullImageName),
		AccountID:           callerAuthID,
		RegisteryToken:      registryToken,
	}, nil
}
