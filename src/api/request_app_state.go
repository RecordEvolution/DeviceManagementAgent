package api

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/safe"
	"strconv"

	"github.com/rs/zerolog/log"
)

func (ex *External) requestAppStateHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	payload, err := responseToTransitionPayload(ex.Config, response)
	if err != nil {
		return nil, err
	}

	var privileged bool
	if payload.CurrentState == common.REMOVED || payload.CurrentState == common.UNINSTALLED {
		privileged, err = ex.Privilege.Check("INSTALL", response.Details)
		if err != nil {
			return nil, err
		}
	} else {
		privileged, err = ex.Privilege.Check("OPERATE", response.Details)
		if err != nil {
			return nil, err
		}
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to request app state"))
	}

	// TODO: remove check and implement proper stream canceling for Docker Compose
	if payload.CancelTransition && payload.DockerCompose != nil {
		return nil, errors.New("canceling docker compose apps not yet implemented")
	}

	err = ex.AppManager.CreateOrUpdateApp(payload)
	if err != nil {
		return nil, err
	}

	safe.Go(func() {
		err = ex.AppManager.RequestAppState(payload)
		if err != nil {
			log.Error().Err(err).Msgf("failed to request app state")
			return
		}
	})

	return &messenger.InvokeResult{}, nil
}

// responseToTransitionPayload parses a Messenger response to a generic common.TransitionPayload struct.
// Values that were not provided will be nil.
func responseToTransitionPayload(config *config.Config, result messenger.Result) (common.TransitionPayload, error) {
	kwargs := result.ArgumentsKw
	// details := result.Details

	appKeyKw := kwargs["app_key"]
	appNameKw := kwargs["app_name"]
	stageKw := kwargs["stage"]
	requestedStateKw := kwargs["target_state"]
	manuallyRequestedStateKw := kwargs["manually_requested_state"]
	releaseKeyKw := kwargs["release_key"]
	newReleaseKeyKw := kwargs["new_release_key"]
	currentStateKw := kwargs["current_state"]
	currentStateKw2 := kwargs["state"]
	requestorAccountKeyKw := kwargs["requestor_account_key"]
	requestorAccountKeyKw2 := kwargs["account_id"]
	environmentKw := kwargs["environment"]
	portsKw := kwargs["ports"]
	// dtaKeyKw := kwargs["device_to_app_key"]
	versionKw := kwargs["version"]
	presentVersionKw := kwargs["present_version"]
	newestVersionKw := kwargs["newest_version"]
	requestUpdateKw := kwargs["request_update"]
	cancelTransitionKw := kwargs["cancel_transition"]
	environmentTemplateKw := kwargs["environment_template"]
	dockerComposeKw := kwargs["docker_compose"]
	newDockerComposeKw := kwargs["new_docker_compose"]

	var appKey uint64
	var releaseKey uint64
	var newReleaseKey uint64
	var requestorAccountKey uint64
	var requestorAccountKey2 uint64
	var appName string
	var stage string
	var requestedState string
	var manuallyRequestedState string
	var currentState string
	var currentState2 string
	var version string
	var presentVersion string
	var newestVersion string
	var requestUpdate bool
	var cancelTransition bool
	var ok bool
	var ports []interface{}
	var environmentTemplate map[string]interface{}
	var environment map[string]interface{}
	var dockerCompose map[string]interface{}
	var newDockerCompose map[string]interface{}

	// TODO: can be simplified with parser function, but unneccessary
	if appKeyKw != nil {
		appKey, ok = appKeyKw.(uint64)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w app_key", errdefs.ErrFailedToParse)
		}
		if appKey == 0 {
			return common.TransitionPayload{}, fmt.Errorf("%w app_key", errdefs.ErrMissingFromPayload)
		}
	}

	if appNameKw != nil {
		appName, ok = appNameKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w appName", errdefs.ErrFailedToParse)
		}
		if appName == "" {
			return common.TransitionPayload{}, fmt.Errorf("%w appName", errdefs.ErrFailedToParse)
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w stage", errdefs.ErrFailedToParse)
		}
		if stage == "" {
			return common.TransitionPayload{}, fmt.Errorf("%w stage", errdefs.ErrMissingFromPayload)
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w requestedState", errdefs.ErrFailedToParse)
		}
	}

	if manuallyRequestedStateKw != nil {
		manuallyRequestedState, ok = manuallyRequestedStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w manuallyRequestedState", errdefs.ErrFailedToParse)
		}
	}

	if currentStateKw != nil {
		currentState, ok = currentStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w currentState", errdefs.ErrFailedToParse)
		}
	}

	if currentStateKw2 != nil {
		currentState2, ok = currentStateKw2.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w currentState2", errdefs.ErrFailedToParse)
		}
	}

	if requestorAccountKeyKw != nil {
		requestorAccountKey, ok = requestorAccountKeyKw.(uint64)
		if !ok {
			requestorAccountKeyString, ok := requestorAccountKeyKw.(string)
			if !ok {
				return common.TransitionPayload{}, fmt.Errorf("%w requestorAccountKey", errdefs.ErrFailedToParse)
			}

			value, err := strconv.Atoi(requestorAccountKeyString)
			if err != nil {
				return common.TransitionPayload{}, err
			}
			requestorAccountKey = uint64(value)
		}
	}

	if requestorAccountKeyKw2 != nil {
		requestorAccountKey2, ok = requestorAccountKeyKw2.(uint64)
		if !ok {
			requestorAccountKeyString2, ok := requestorAccountKeyKw2.(string)
			if !ok {
				return common.TransitionPayload{}, fmt.Errorf("%w requestorAccountKey2", errdefs.ErrFailedToParse)
			}

			value, err := strconv.Atoi(requestorAccountKeyString2)
			if err != nil {
				return common.TransitionPayload{}, err
			}
			requestorAccountKey2 = uint64(value)
		}
	}

	if releaseKeyKw != nil {
		releaseKey, ok = releaseKeyKw.(uint64)
		if !ok {
			releaseKeyString, ok := releaseKeyKw.(string)
			if !ok {
				return common.TransitionPayload{}, fmt.Errorf("%w releaseKeyString", errdefs.ErrFailedToParse)
			}

			// due to a bug the release key can be stored as string...
			parsedReleaseKey, err := strconv.ParseUint(releaseKeyString, 10, 64)
			if err != nil {
				return common.TransitionPayload{}, err
			}

			releaseKey = parsedReleaseKey
		}
	}

	if newReleaseKeyKw != nil {
		newReleaseKey, ok = newReleaseKeyKw.(uint64)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w newReleaseKey", errdefs.ErrFailedToParse)
		}
	}

	if versionKw != nil {
		version, ok = versionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w version", errdefs.ErrFailedToParse)
		}
	}

	if newestVersionKw != nil {
		newestVersion, ok = newestVersionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w newestVersion", errdefs.ErrFailedToParse)
		}
	}

	if presentVersionKw != nil {
		presentVersion, ok = presentVersionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w presentVersion", errdefs.ErrFailedToParse)
		}
	}

	if requestUpdateKw != nil {
		requestUpdate, ok = requestUpdateKw.(bool)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w requestUpdate", errdefs.ErrFailedToParse)
		}
	}

	if cancelTransitionKw != nil {
		cancelTransition, ok = cancelTransitionKw.(bool)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w cancelTransition", errdefs.ErrFailedToParse)
		}
	}

	if environmentKw != nil {
		environment, ok = environmentKw.(map[string]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w environment", errdefs.ErrFailedToParse)
		}
	}

	if environmentTemplateKw != nil {
		environmentTemplate, ok = environmentTemplateKw.(map[string]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w environment template", errdefs.ErrFailedToParse)
		}
	}

	if portsKw != nil {
		ports, ok = portsKw.([]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w ports", errdefs.ErrFailedToParse)
		}
	}

	if dockerComposeKw != nil {
		dockerCompose, ok = dockerComposeKw.(map[string]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w docker compose", errdefs.ErrFailedToParse)
		}
	}

	if newDockerComposeKw != nil {
		newDockerCompose, ok = newDockerComposeKw.(map[string]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("%w new docker compose", errdefs.ErrFailedToParse)
		}
	}

	// callerAuthIDString := details["caller_authid"]

	// callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))

	// callerAuthID, ok := callerAuthIDString.(string)
	// if !ok {
	// 	return common.TransitionPayload{}, fmt.Errorf("Failed to parse callerAuthid")
	// }

	// happens where there is no app yet, for example, when you press stop on first build
	if requestedState == "" && manuallyRequestedState != "" {
		requestedState = manuallyRequestedState
	}

	if currentState == "" && currentState2 != "" {
		currentState = currentState2
	}

	if requestorAccountKey == 0 && requestorAccountKey2 != 0 {
		requestorAccountKey = requestorAccountKey2
	}

	payload := common.BuildTransitionPayload(appKey, appName, requestorAccountKey,
		common.Stage(stage), common.AppState(currentState),
		common.AppState(requestedState), releaseKey, newReleaseKey, config,
	)

	payload.RequestUpdate = requestUpdate

	// Version used to publish a release
	payload.Version = version

	// Newest version that is available of app
	payload.NewestVersion = newestVersion

	// Version that is currently on the device
	payload.PresentVersion = presentVersion

	payload.EnvironmentVariables = environment
	payload.EnvironmentTemplate = environmentTemplate

	payload.Ports = ports
	payload.DockerCompose = dockerCompose
	payload.NewDockerCompose = newDockerCompose

	payload.CancelTransition = cancelTransition

	// registryToken is added before we transition state and is not part of the response payload
	return payload, nil
}
