package api

import (
	"context"
	"fmt"
	"reagent/apps"
	"reagent/common"
	"reagent/config"
	"reagent/container"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/network"
	"reagent/persistence"
	"reagent/privilege"
	"reagent/system"
	"reagent/terminal"
	"reagent/tunnel"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// External is the API that is meant to be used by the externally exposed WAMP topics.
// It contains all the functionality available in the reagent.
type External struct {
	Container       container.Container
	Messenger       messenger.Messenger
	LogMessenger    messenger.Messenger
	Database        persistence.Database
	TunnelManager   tunnel.TunnelManager
	Network         network.Network
	Privilege       *privilege.Privilege
	Filesystem      *filesystem.Filesystem
	System          *system.System
	AppManager      *apps.AppManager
	TerminalManager *terminal.TerminalManager
	LogManager      *logging.LogManager
	Config          *config.Config
}

// RegistrationHandler is the handler that gets executed whenever a registered topic gets called.
type RegistrationHandler = func(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error)

// ! dynamically created registrations (terminal / logger) can be found in their respective packages
func (ex *External) getTopicHandlerMap() map[topics.Topic]RegistrationHandler {
	return map[topics.Topic]RegistrationHandler{
		topics.RequestAppState:        ex.requestAppStateHandler,
		topics.WriteToFile:            ex.writeToFileHandler,
		topics.Handshake:              ex.deviceHandshakeHandler,
		topics.GetImages:              ex.getImagesHandler,
		topics.RequestTerminalSession: ex.requestTerminalSessHandler,
		topics.StartTerminalSession:   ex.startTerminalSessHandler,
		topics.StopTerminalSession:    ex.stopTerminalSession,

		topics.ListWiFiNetworks:        ex.listWiFiNetworksHandler,
		topics.AddWiFiConfiguration:    ex.addWiFiConfigurationHandler,
		topics.ScanWifiNetworks:        ex.wifiScanHandler,
		topics.RemoveWiFiConfiguration: ex.removeWifiHandler,
		topics.SelectWiFiNetwork:       ex.selectWiFiNetworkHandler,
		topics.ListEthernetDevices:     ex.listEthernetDevices,
		topics.UpdateIPv4Configuration: ex.updateIPConfigHandler,
		topics.SystemReboot:            ex.systemRebootHandler,
		topics.SystemShutdown:          ex.systemShutdownHandler,
		topics.RestartWifi:             ex.wifiRebootHandler,
		topics.UpdateAgent:             ex.updateReagent,
		topics.PruneImages:             ex.pruneImageHandler,
		topics.GetAgentMetaData:        ex.getAgentMetadataHandler,
		topics.ListContainers:          ex.listContainersHandler,
		topics.GetAgentLogs:            ex.getAgentLogs,
		topics.GetNetworkMetaData:      ex.getNetworkDataHandler,
		topics.GetAppLogHistory:        ex.getAppLogHistoryHandler,

		topics.GetOSRelease:     ex.getOSReleaseHandler,
		topics.DownloadOSUpdate: ex.downloadOSUpdateHandler,
		topics.InstallOSUpdate:  ex.installOSUpdateHandler,

		topics.ExecuteCommand:     ex.codeExecutionHandler,
		topics.InitDeviceTerminal: ex.initDeviceTerm,
		topics.GetIPv4Addresses:   ex.getCurrentIPAddresses,
	}
}

func wrapDetails(handler RegistrationHandler) RegistrationHandler {
	return func(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
		if response.Details["caller_authid"] == "system" {
			requestorAccountKeyKw := response.ArgumentsKw["requestor_account_key"]

			if requestorAccountKeyKw == nil {
				if len(response.Arguments) == 0 {
					return handler(ctx, response)
				}

				argsOne := response.Arguments[0]
				argsDict, ok := argsOne.(common.Dict)
				if !ok {
					return handler(ctx, response)
				}

				argsRequestorAccountKey := argsDict["requestor_account_key"]
				if argsRequestorAccountKey != nil {
					requestorAccountKeyKw = argsRequestorAccountKey
				} else {
					return handler(ctx, response)
				}
			}

			value, err := strconv.Atoi(fmt.Sprint(requestorAccountKeyKw))
			if err == nil {
				response.Details["caller_authid"] = uint64(value)
			}
		}

		return handler(ctx, response)
	}
}

// RegisterAll registers all the static topics exposed by the reagent
func (ex *External) RegisterAll() error {
	serialNumber := ex.Config.ReswarmConfig.SerialNumber
	topicHandlerMap := ex.getTopicHandlerMap()
	for topic, handler := range topicHandlerMap {
		// will register all topics, e.g.: re.mgmt.request_app_state
		fullTopic := common.BuildExternalApiTopic(serialNumber, string(topic))
		err := ex.Messenger.Register(topics.Topic(fullTopic), wrapDetails(handler), nil)
		if err != nil {
			// on reconnect we will reregister, which could cause a already exists exception
			if strings.Contains(err.Error(), "wamp.error.procedure_already_exists") {
				log.Warn().Msgf("Tried to register already existing topic: %s", fullTopic)
			} else {
				return err
			}
		}
		log.Info().Msgf("Registered topic %s on the device", fullTopic)
	}
	return nil
}
