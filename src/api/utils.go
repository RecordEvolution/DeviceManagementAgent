package api

import (
	"fmt"
	"reagent/api/common"
	"reagent/apps"
	"reagent/config"
	"reagent/messenger"
	"strings"
)

// ResponseToTransitionPayload parses a Messenger response to a generic TransitionPayload struct.
// Values that were not provided will be nil.
func ResponseToTransitionPayload(config *config.ReswarmConfig, result messenger.Result) (apps.TransitionPayload, error) {
	kwargs := result.ArgumentsKw
	details := result.Details

	appKeyKw := kwargs["app_key"]
	appNameKw := kwargs["app_name"]
	stageKw := kwargs["stage"]
	requestedStateKw := kwargs["requested_state"]

	var appKey uint64
	var appName string
	var stage string
	var requestedState string

	var ok bool

	// TODO: can be simplified with parser function, but unneccessary
	if appKeyKw != nil {
		appKey, ok = appKeyKw.(uint64)
		if !ok {
			return apps.TransitionPayload{}, fmt.Errorf("Failed to parse app_key")
		}
	}

	if appNameKw != nil {
		appName, ok = appNameKw.(string)
		if !ok {
			return apps.TransitionPayload{}, fmt.Errorf("Failed to parse appName")
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return apps.TransitionPayload{}, fmt.Errorf("Failed to parse stage")
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return apps.TransitionPayload{}, fmt.Errorf("Failed to parse requested_state")
		}
	}

	callerAuthIDString := details["caller_authid"]

	// callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))

	callerAuthID, ok := callerAuthIDString.(string)
	if !ok {
		return apps.TransitionPayload{}, fmt.Errorf("Failed to parse callerAuthid")
	}

	containerName := fmt.Sprintf("%s_%d_%s", stage, appKey, appName)
	imageName := fmt.Sprintf("%s_%s_%d_%s", stage, config.Architecture, appKey, appName)
	fullImageName := fmt.Sprintf("%s/%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName)

	return apps.TransitionPayload{
		Stage:               common.Stage(stage),
		RequestedState:      common.AppState(requestedState),
		AppName:             appName,
		AppKey:              appKey,
		ContainerName:       strings.ToLower(containerName),
		ImageName:           imageName,
		RepositoryImageName: strings.ToLower(fullImageName),
		AccountID:           callerAuthID,
	}, nil
}
