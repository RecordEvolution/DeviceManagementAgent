package api

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/config"
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

	safe.Go(func() {
		err = ex.AppManager.CreateOrUpdateApp(payload)
		if err != nil {
			log.Error().Stack().Err(err)
			return
		}

		err = ex.AppManager.RequestAppState(payload)
		if err != nil {
			log.Error().Stack().Err(err)
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
	requestorAccountKeyKw := kwargs["requestor_account_key"]
	environmentKw := kwargs["environment"]
	// dtaKeyKw := kwargs["device_to_app_key"]
	versionKw := kwargs["version"]
	presentVersionKw := kwargs["present_version"]
	newestVersionKw := kwargs["newest_version"]
	requestUpdateKw := kwargs["request_update"]
	cancelTransitionKw := kwargs["cancel_transition"]

	var appKey uint64
	var releaseKey uint64
	var newReleaseKey uint64
	var requestorAccountKey uint64
	var appName string
	var stage string
	var requestedState string
	var manuallyRequestedState string
	var currentState string
	var version string
	var presentVersion string
	var newestVersion string
	var requestUpdate bool
	var cancelTransition bool
	var ok bool
	var environment map[string]interface{}

	// TODO: can be simplified with parser function, but unneccessary
	if appKeyKw != nil {
		appKey, ok = appKeyKw.(uint64)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse app_key")
		}
		if appKey == 0 {
			return common.TransitionPayload{}, fmt.Errorf("app_key is missing from payload")
		}
	}

	if appNameKw != nil {
		appName, ok = appNameKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse appName")
		}
		if appName == "" {
			return common.TransitionPayload{}, fmt.Errorf("app_name is missing from payload")
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse stage")
		}
		if stage == "" {
			return common.TransitionPayload{}, fmt.Errorf("stage is missing from payload")
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse requested_state")
		}
	}

	if manuallyRequestedStateKw != nil {
		manuallyRequestedState, ok = manuallyRequestedStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse manually requested state")
		}
	}

	if currentStateKw != nil {
		currentState, ok = currentStateKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse currentState")
		}
	}

	if requestorAccountKeyKw != nil {
		requestorAccountKey, ok = requestorAccountKeyKw.(uint64)
		if !ok {
			requestorAccountKeyString, ok := requestorAccountKeyKw.(string)
			if !ok {
				return common.TransitionPayload{}, fmt.Errorf("Failed to parse requestor_account_key")
			}

			value, err := strconv.Atoi(requestorAccountKeyString)
			if err != nil {
				return common.TransitionPayload{}, err
			}
			requestorAccountKey = uint64(value)
		}
	}

	if releaseKeyKw != nil {
		releaseKey, ok = releaseKeyKw.(uint64)
		if !ok {
			releaseKeyString, ok := releaseKeyKw.(string)
			if !ok {
				return common.TransitionPayload{}, fmt.Errorf("Failed to parse release_key")
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
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse new_release_key")
		}
	}

	if versionKw != nil {
		version, ok = versionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse version")
		}
	}

	if newestVersionKw != nil {
		newestVersion, ok = newestVersionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse newest_version")
		}
	}

	if presentVersionKw != nil {
		presentVersion, ok = presentVersionKw.(string)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse present_version")
		}
	}

	if requestUpdateKw != nil {
		requestUpdate, ok = requestUpdateKw.(bool)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse request_update")
		}
	}

	if cancelTransitionKw != nil {
		cancelTransition, ok = cancelTransitionKw.(bool)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse cancel_transition")
		}
	}

	if environmentKw != nil {
		environment, ok = environmentKw.(map[string]interface{})
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse environment")
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

	payload.CancelTransition = cancelTransition

	// registryToken is added before we transition state and is not part of the response payload
	return payload, nil
}
