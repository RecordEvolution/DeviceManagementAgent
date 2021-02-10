package api

import (
	"context"
	"reagent/apps"
	"reagent/common"
	"reagent/config"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
	"reagent/terminal"
)

// External is the API that is meant to be used by the externally exposed WAMP topics.
// It contains all the functionality available in the reagent.
type External struct {
	Messenger       messenger.Messenger
	StateStorer     persistence.StateStorer
	StateMachine    *apps.StateMachine
	StateUpdater    *apps.StateUpdater
	TerminalManager *terminal.TerminalManager
	LogManager      *logging.LogManager
	Config          *config.Config
}

func (ex *External) getTopicHandlerMap() map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return map[string]func(ctx context.Context, response messenger.Result) messenger.InvokeResult{
		string(common.TopicRequestAppState): ex.requestAppStateHandler,
		string(common.TopicWriteToFile):     ex.writeToFileHandler,
		string(common.TopicHandshake):       ex.deviceHandshakeHandler,
		string(common.TopicContainerImages): ex.getImagesHandler,
		string(common.TopicStartTerminal):   ex.startTerminalHandler,
	}
}

// RegisterAll registers all the RPCs/Subscriptions exposed by the reagent
func (ex *External) RegisterAll() {
	serialNumber := ex.Config.ReswarmConfig.SerialNumber
	topicHandlerMap := ex.getTopicHandlerMap()
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := common.BuildExternalApiTopic(serialNumber, topic)
		ex.Messenger.Register(fullTopic, handler, nil)
	}
}
