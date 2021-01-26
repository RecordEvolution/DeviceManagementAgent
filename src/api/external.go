package api

import (
	"context"
	"fmt"
	"reagent/api/common"
	"reagent/apps"
	"reagent/filesystem"
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
		string(common.TopicWriteToFile):     ex.writeToFileHandler,
	}
}

func (ex *External) writeToFileHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	args := response.Arguments

	// Matches file_transfer.ts payload
	chunkArg := args[0]
	// appTypeArg := args[1]
	nameArg := args[2]
	// container_nameArg := args[3]
	// totalArg := args[4]

	name, ok := nameArg.(string)
	if !ok {
		return messenger.InvokeResult{Err: fmt.Sprintf("Failed to parse name argument %s", nameArg)}
	}

	chunk, ok := chunkArg.(string)
	if !ok {
		return messenger.InvokeResult{Err: fmt.Sprintf("Failed to parse chunk argument %s", chunkArg)}
	}

	filePath := "/reswarm_app_builds" // TODO: load from generalized config
	err := filesystem.Write(name, filePath, chunk)

	if err != nil {
		return messenger.InvokeResult{Err: err.Error()}
	}

	return messenger.InvokeResult{}
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
