package api

import (
	"context"
	"reagent/apps"
	"reagent/common"
	"reagent/config"
	"reagent/logging"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"
	"reagent/terminal"
)

// External is the API that is meant to be used by the externally exposed WAMP topics.
// It contains all the functionality available in the reagent.
type External struct {
	Messenger       messenger.Messenger
	Database        persistence.Database
	AppManager      *apps.AppManager
	TerminalManager *terminal.TerminalManager
	LogManager      *logging.LogManager
	Config          *config.Config
}

// RegistrationHandler is the handler that gets executed whenever a registered topic gets called.
type RegistrationHandler = func(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error)

//! dynamically created registrations (terminal / logger) can be found in their respective packages
func (ex *External) getTopicHandlerMap() map[topics.Topic]RegistrationHandler {
	return map[topics.Topic]RegistrationHandler{
		topics.RequestAppState:        ex.requestAppStateHandler,
		topics.WriteToFile:            ex.writeToFileHandler,
		topics.Handshake:              ex.deviceHandshakeHandler,
		topics.ContainerImages:        ex.getImagesHandler,
		topics.RequestTerminalSession: ex.requestTerminalSessHandler,
		topics.StartTerminalSession:   ex.startTerminalSessHandler,
		topics.StopTerminalSession:    ex.stopTerminalSession,

		topics.ListWiFiNetworks:       ex.listWiFiNetworksHandler,
		topics.AddWiFiConfiguration:   ex.addWiFiConfigurationHandler,
		topics.SelectWiFiNetwork:      ex.selectWiFiNetworkHandler,
		topics.SystemReboot:           ex.systemRebootHandler,
		topics.SystemShutdown:         ex.systemShutdownHandler,
	}
}

// RegisterAll registers all the static topics exposed by the reagent
func (ex *External) RegisterAll() {
	serialNumber := ex.Config.ReswarmConfig.SerialNumber
	topicHandlerMap := ex.getTopicHandlerMap()
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := common.BuildExternalApiTopic(serialNumber, string(topic))
		ex.Messenger.Register(topics.Topic(fullTopic), handler, nil)
	}
}
