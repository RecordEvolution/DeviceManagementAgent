package api

import (
	"context"
	"fmt"
	"reagent/apps"
	"reagent/common"
	"reagent/config"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
)

type External struct {
	Messenger    messenger.Messenger
	StateMachine apps.StateMachine
	StateStorer  persistence.StateStorer
	LogManager   logging.LogManager
	Config       config.Config
}

const topicPrefix = "re.mgmt"

func (ex *External) getTopicHandlerMap() map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult{
		string(common.TopicRequestAppState): requestAppStateHandler(ex),
		string(common.TopicWriteToFile):     writeToFileHandler(ex),
		string(common.TopicHandshake):       deviceHandshakeHandler(ex),
		string(common.TopicContainerImages):          getImagesHandler(ex),
	}
}

// RegisterAll registers all the RPCs/Subscriptions exposed by the reagent
func (ex *External) RegisterAll() {
	serialNumber := ex.Config.ReswarmConfig.SerialNumber
	topicHandlerMap := ex.getTopicHandlerMap()
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topic)
		ex.Messenger.Register(fullTopic, handler, nil)
	}
}
