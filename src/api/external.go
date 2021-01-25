package api

import (
	"context"
	"fmt"
	"reagent/apps"
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

func (ex *External) requestAppStateHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	kwargs := response.ArgumentsKw
	details := response.Details

	appKey := kwargs["app_key"]
	appName := kwargs["app_name"]
	stage := kwargs["stage"]
	arch := kwargs["arch"]
	requestedState := kwargs["requested_state"]

	// Details is Map<string, string>
	callerAuthIDString := details["caller_authid"]
	callerAuthID, err := strconv.Atoi(callerAuthIDString.(string))
	if err != nil {
		panic(err)
	}

	fmt.Printf("request_app_state request with: appKey: %d, appName: %s, requestedState: %s", appKey, appName, requestedState)
	fmt.Println()

	config := ex.Messenger.GetConfig()
	containerName := fmt.Sprintf("%s_%d_%s", stage, appKey, appName)
	imageName := fmt.Sprintf("%s_%s_%d_%s", stage, arch, appKey, appName)
	fullImageName := fmt.Sprintf("%s/%s/%s", config.DockerRegistryURL, config.DockerMainRepository, imageName)
	transitionPayload := apps.TransitionPayload{
		Stage:         apps.Stage(stage.(string)),
		AppName:       appName.(string),
		AppKey:        appKey.(uint64),
		ContainerName: strings.ToLower(containerName),
		ImageName:     imageName,
		FullImageName: fullImageName,
		AccountID:     callerAuthID,
	}

	err = ex.StateMachine.RequestAppState(transitionPayload, apps.AppState(requestedState.(string)))
	if err != nil {
		panic(err)
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
