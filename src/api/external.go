package api

import (
	"context"
	"fmt"
	"reagent/apps"
	"reagent/fs"
	"reagent/messenger"
	"strconv"
	"strings"
)

type External struct {
	Messenger    messenger.Messenger
	StateMachine *apps.StateMachine
}

const topicPrefix = "re.mgmt"

func (ex *External) getTopicHandlerMap() map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult{
		string(RequestAppState): ex.requestAppStateHandler,
	}
}

func responseToTransitionPayload(config *fs.ReswarmConfig, result messenger.Result) (apps.TransitionPayload, error) {
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
			return apps.TransitionPayload{}, nil
		}
	}

	if appNameKw != nil {
		appName, ok = appKeyKw.(string)
		if !ok {
			return apps.TransitionPayload{}, nil
		}
	}

	if stageKw != nil {
		stage, ok = stageKw.(string)
		if !ok {
			return apps.TransitionPayload{}, nil
		}
	}

	if archKw != nil {
		arch, ok = archKw.(string)
		if !ok {
			return apps.TransitionPayload{}, nil
		}
	}

	if requestedStateKw != nil {
		requestedState, ok = requestedStateKw.(string)
		if !ok {
			return apps.TransitionPayload{}, nil
		}
	}

	callerAuthIDString := details["caller_authid"]
	callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))
	if err != nil {
		return apps.TransitionPayload{}, err
	}

	containerName := fmt.Sprintf("%s_%d_%s", stage, appKey, appName)
	imageName := fmt.Sprintf("%s_%s_%d_%s", stage, arch, appKey, appName)
	fullImageName := fmt.Sprintf("%s/%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName)

	return apps.TransitionPayload{
		Stage:          apps.Stage(stage),
		RequestedState: apps.AppState(requestedState),
		AppName:        appName,
		AppKey:         appKey,
		ContainerName:  strings.ToLower(containerName),
		ImageName:      imageName,
		FullImageName:  strings.ToLower(fullImageName),
		AccountID:      callerAuthID,
	}, nil
}

func (ex *External) requestAppStateHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	config := ex.Messenger.GetConfig()
	transitionPayload, err := responseToTransitionPayload(config, response)

	err = ex.StateMachine.RequestAppState(transitionPayload)
	if err != nil {
		return messenger.InvokeResult{Err: err.Error()}
	}

	return messenger.InvokeResult{} // Return empty result
}

// RegisterAll registers all the RPCs/Subscriptions exposed by the reagent
func (ex *External) RegisterAll() {
	serialNumber := ex.Messenger.GetConfig().SerialNumber
	topicHandlerMap := ex.getTopicHandlerMap()
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topic)
		ex.Messenger.Register(fullTopic, handler, nil)
	}
}
