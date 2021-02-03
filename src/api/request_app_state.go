package api

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"strconv"

	"github.com/gammazero/nexus/v3/wamp"
)

func (ex *External) requestAppStateHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	payload, err := responseToTransitionPayload(ex.Config, response)
	if err != nil {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			// TODO: show different URI error based on error that was returned upwards
			Err: string(wamp.ErrInvalidArgument),
		}
	}

	// TODO: only fetch token when it's required
	// Before executing the state transition, fetch a registry token when required
	token, err := ex.StateUpdater.GetRegistryToken(payload.RequestorAccountKey)
	if err != nil {
		fmt.Println("failed to get registryToken", err)
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	err = ex.StateStorer.UpsertRequestedStateChange(payload)
	if err != nil {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	payload.RegisteryToken = token
	err = ex.StateMachine.RequestAppState(payload)
	if err != nil {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	return messenger.InvokeResult{} // Return empty result
}

// responseToTransitionPayload parses a Messenger response to a generic common.TransitionPayload struct.
// Values that were not provided will be nil.
func responseToTransitionPayload(config *config.Config, result messenger.Result) (common.TransitionPayload, error) {
	kwargs := result.ArgumentsKw
	// details := result.Details

	appKeyKw := kwargs["app_key"]
	appNameKw := kwargs["app_name"]
	stageKw := kwargs["stage"]
	requestedStateKw := kwargs["manually_requested_state"]
	// releaseKeyKw := kwargs["release_key"]
	currentStateKw := kwargs["state"]
	requestorAccountKeyKw := kwargs["requestor_account_key"]
	dtaKeyKw := kwargs["device_to_app_key"]
	versionKw := kwargs["version"]
	presentVersionKw := kwargs["present_version"]
	newestVersionKw := kwargs["newest_version"]

	var appKey uint64
	var dtaKey uint64
	var requestorAccountKey uint64
	var appName string
	var stage string
	var requestedState string
	var currentState string
	var version string
	var presentVersion string
	var newestVersion string
	var ok bool

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

	if dtaKeyKw != nil {
		dtaKey, ok = dtaKeyKw.(uint64)
		if !ok {
			return common.TransitionPayload{}, fmt.Errorf("Failed to parse device_to_app_key")
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

	// callerAuthIDString := details["caller_authid"]

	// callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))

	// callerAuthID, ok := callerAuthIDString.(string)
	// if !ok {
	// 	return common.TransitionPayload{}, fmt.Errorf("Failed to parse callerAuthid")
	// }

	payload := common.BuildTransitionPayload(
		dtaKey, appKey, appName, requestorAccountKey,
		common.Stage(stage), common.AppState(currentState),
		common.AppState(requestedState), config,
	)

	// Not always part of the payload
	payload.Version = version
	payload.NewestVersion = newestVersion
	payload.PresentVersion = presentVersion

	// registryToken is added before we transition state and is not part of the response payload
	return payload, nil
}
