package api

import (
	"context"
	"fmt"
	"reagent/messenger"
)

const topicPrefix = "re.mgmt"

var topicHandlerMap = map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult{
	string(RequestAppState): requestAppStateHandler,
}

func requestAppStateHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	kwargs := response.ArgumentsKw
	appKey := kwargs["app_key"]
	appName := kwargs["app_name"]
	requestedState := kwargs["requested_state"]
	fmt.Printf("request_app_state request with: appKey: %d, appName: %s, requestedState: %s", appKey, appName, requestedState)

	return messenger.InvokeResult{} // Return empty result
}

// RegisterAll registers all the RPCs exposed by the reagent
func RegisterAll(msg messenger.Messenger) {
	serialNumber := msg.GetConfig().SerialNumber
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topic)
		msg.Register(fullTopic, handler, nil)
	}
}
