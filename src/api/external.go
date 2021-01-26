package api

import (
	"context"
	"fmt"
	"reagent/api/common"
	"reagent/apps"
	"reagent/messenger"
)

type External struct {
	Messenger    messenger.Messenger
	StateMachine apps.StateMachine
}

const topicPrefix = "re.mgmt"

func (ex *External) getTopicHandlerMap() map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult{
		string(common.TopicRequestAppState): ex.requestAppStateHandler,
	}
}

func (ex *External) requestAppStateHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	config := ex.Messenger.GetConfig()
	transitionPayload, err := apps.ResponseToTransitionPayload(config, response)
	if err != nil {
		return messenger.InvokeResult{Err: err.Error()}
	}
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
