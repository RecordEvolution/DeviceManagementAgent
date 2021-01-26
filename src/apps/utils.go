package apps

import (
	"fmt"
	"reagent/api/common"
	"reagent/config"
	"reagent/messenger"
	"regexp"
	"strings"
)

// ResponseToTransitionPayload parses a Messenger response to a generic TransitionPayload struct.
// Values that were not provided will be nil.
func ResponseToTransitionPayload(config *config.ReswarmConfig, result messenger.Result) (TransitionPayload, error) {
	kwargs := result.ArgumentsKw
	details := result.Details

	appKeyKw := kwargs["app_key"]
	appNameKw := kwargs["app_name"]
	stageKw := kwargs["stage"]
	archKw := kwargs["arch"]
	requestedStateKw := kwargs["requested_state"]

	var appKey uint64
	var appName string
	var stage string
	var arch string
	var requestedState string

	var ok bool

	// TODO: can be simplified with parser function, but unneccessary
	if appKeyKw != nil {
		appKey, ok = appKeyKw.(uint64)
		if !ok {
			return TransitionPayload{}, fmt.Errorf("Failed to parse app_key")
		}
	}

	if appNameKw != nil {
		appName, ok = appNameKw.(string)
		if !ok {
			return TransitionPayload{}, fmt.Errorf("Failed to parse appName")
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return TransitionPayload{}, fmt.Errorf("Failed to parse stage")
		}
	}

	if archKw != nil {
		arch, ok = archKw.(string)
		if !ok {
			return TransitionPayload{}, fmt.Errorf("Failed to parse arch")
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return TransitionPayload{}, fmt.Errorf("Failed to parse requested_state")
		}
	}

	callerAuthIDString := details["caller_authid"]

	// callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))

	callerAuthID, ok := callerAuthIDString.(string)
	if !ok {
		return TransitionPayload{}, fmt.Errorf("Failed to parse callerAuthid")
	}

	containerName := fmt.Sprintf("%s_%d_%s", stage, appKey, appName)
	imageName := fmt.Sprintf("%s_%s_%d_%s", stage, arch, appKey, appName)
	fullImageName := fmt.Sprintf("%s/%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName)

	return TransitionPayload{
		Stage:          common.Stage(stage),
		RequestedState: common.AppState(requestedState),
		AppName:        appName,
		AppKey:         appKey,
		ContainerName:  strings.ToLower(containerName),
		ImageName:      imageName,
		FullImageName:  strings.ToLower(fullImageName),
		AccountID:      callerAuthID,
	}, nil
}

func ParseExitCodeFromStatus(status string) string {
	statusString := regexp.MustCompile(`\((.*?)\)`).FindString(status)
	return strings.TrimRight(strings.TrimLeft(statusString, "("), ")")
}
