package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"
	"reagent/safe"
	"reagent/store"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type LogType string
type LoggerCommand string

const LOGGER_CLEAR LoggerCommand = "LOGGER_CLEAR"
const CONTAINER LogType = "CONTAINER"
const AGENT LogType = "AGENT"

type LogEntry struct {
	entry   string
	logType LogType
}

type LogSubscription struct {
	SubscriptionID         string // The currently active subscriptionID for this log entry, can be empty
	ContainerName          string
	Stream                 io.ReadCloser
	ChannelStream          chan string
	logHistory             []*LogEntry
	Publish                bool // Publish defines if we are currently publishing the logs
	Active                 bool // Active defines wether or not we are currently iterating over the log stream
	logEntriesMutex        sync.Mutex
	subscriptionStateMutex sync.Mutex
}

func logEntriesToString(logEntries []*LogEntry) []string {
	var logs []string

	for _, log := range logEntries {
		logs = append(logs, log.entry)
	}

	return logs
}

func (ls *LogSubscription) appendLog(logEntry LogEntry) {
	ls.logEntriesMutex.Lock()
	ls.logHistory = append(ls.logHistory, &logEntry)
	ls.logEntriesMutex.Unlock()
}

type LogManager struct {
	Container              container.Container
	Messenger              messenger.Messenger
	Database               persistence.Database
	AppStore               store.AppStore
	activeLogs             map[string]*LogSubscription
	activeComposeLogs      map[string]*LogSubscription
	composeLogsChannel     chan LogSubscription
	activeLogsMutex        sync.Mutex
	activeComposeLogsMutex sync.Mutex
}

type ErrorChunk struct {
	Error       string      `json:"error"`
	ErrorDetail ErrorDetail `json:"errorDetail"`
}

type ErrorDetail struct {
	Message string `json:"message"`
}

func NewLogManager(cont container.Container, msg messenger.Messenger, db persistence.Database, as store.AppStore) LogManager {
	return LogManager{
		activeLogs:         make(map[string]*LogSubscription),
		activeComposeLogs:  make(map[string]*LogSubscription),
		composeLogsChannel: make(chan LogSubscription),
		Container:          cont,
		Messenger:          msg,
		Database:           db,
		AppStore:           as,
	}
}

// Amount of lines that will be stored for each app
const historyStorageLimit = 100

type JSONProgress struct {
	terminalFd uintptr
	Current    int64  `json:"current,omitempty"`
	Total      int64  `json:"total,omitempty"`
	Start      int64  `json:"start,omitempty"`
	HideCounts bool   `json:"hidecounts,omitempty"`
	Units      string `json:"units,omitempty"`
	nowFunc    func() time.Time
	winSize    int
}

type JSONError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type JSONMessage struct {
	Stream          string           `json:"stream,omitempty"`
	Status          string           `json:"status,omitempty"`
	Progress        *JSONProgress    `json:"progressDetail,omitempty"`
	ProgressMessage string           `json:"progress,omitempty"` // deprecated
	ID              string           `json:"id,omitempty"`
	From            string           `json:"from,omitempty"`
	Time            int64            `json:"time,omitempty"`
	TimeNano        int64            `json:"timeNano,omitempty"`
	Error           *JSONError       `json:"errorDetail,omitempty"`
	ErrorMessage    string           `json:"error,omitempty"` // deprecated
	Aux             *json.RawMessage `json:"aux,omitempty"`
}

func (lm *LogManager) SetMessenger(messenger messenger.Messenger) {
	lm.Messenger = messenger
}

func (lm *LogManager) ClearRemote(containerName string) error {
	return lm.Publish(containerName, string(LOGGER_CLEAR))
}

func (lm *LogManager) ClearLogHistory(containerName string) error {
	lm.activeLogsMutex.Lock()
	activeLogEntry := lm.activeLogs[containerName]
	lm.activeLogsMutex.Unlock()

	if activeLogEntry != nil {
		// clear locally
		activeLogEntry.subscriptionStateMutex.Lock()
		activeLogEntry.logHistory = make([]*LogEntry, 0)
		activeLogEntry.subscriptionStateMutex.Unlock()
	}

	safe.Go(func() {
		stage, appKey, appName, err := common.ParseContainerName(containerName)
		if err != nil {
			return
		}

		// clear in database
		lm.Database.ClearAllLogHistory(appName, appKey, common.Stage(stage))
	})

	return nil
}

func (lm *LogManager) emitChannelStream(logEntry *LogSubscription) error {
	if logEntry.ChannelStream == nil {
		return nil
	}

	// Already watching logs, should just return
	logEntry.subscriptionStateMutex.Lock()
	if logEntry.Active {
		logEntry.subscriptionStateMutex.Unlock()
		return nil
	}
	logEntry.subscriptionStateMutex.Unlock()

	topic := lm.buildTopic(logEntry.ContainerName)

	// cleanup func, closes the stream + saves the current logs in the database
	defer func() {
		safe.Go(func() {
			logEntry.subscriptionStateMutex.Lock()
			logEntry.Active = false
			logEntry.subscriptionStateMutex.Unlock()

			stage, appKey, appName, err := common.ParseContainerName(logEntry.ContainerName)
			if err != nil {
				return
			}

			logEntry.subscriptionStateMutex.Lock()
			logHistory := logEntry.logHistory

			err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), logEntriesToString(logHistory))
			if err != nil {
				logEntry.subscriptionStateMutex.Unlock()
				return
			}

			logEntry.subscriptionStateMutex.Unlock()

			safe.Go(func() {
				logEntry.subscriptionStateMutex.Lock()
				containerName := logEntry.ContainerName
				logEntry.subscriptionStateMutex.Unlock()

				logs, err := lm.getNonAgentLogs(containerName)
				if err != nil {
					return
				}

				for _, appLog := range logs {
					err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{appLog}, nil, nil)
					if err != nil {
						log.Error().Err(err).Msgf("failed to publish to %s in stream cleanup", topic)
					}
				}
			})

			log.Debug().Msgf("goroutine has finshed following logs for %s", logEntry.ContainerName)
		})
	}()

	logEntry.subscriptionStateMutex.Lock()
	logEntry.Active = true
	logEntry.subscriptionStateMutex.Unlock()

	// var lastChunk string
	// if theres an error it will always be the last chunk of the stream
	for chunk := range logEntry.ChannelStream {
		logEntry.subscriptionStateMutex.Lock()
		if len(logEntry.logHistory) == historyStorageLimit {
			logEntry.logHistory = logEntry.logHistory[1:]
		}

		logEntry.appendLog(LogEntry{entry: chunk, logType: CONTAINER})

		shouldPublish := logEntry.Publish

		logEntry.subscriptionStateMutex.Unlock()

		if shouldPublish {
			err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{chunk}, nil, nil)
			if err != nil {
				log.Error().Err(err).Msgf("failed to publish to %s in publish loop", topic)
			}
		}

		// lastChunk = chunk
	}

	// err := scanner.Err()
	// if err != nil {
	// 	if strings.Contains(err.Error(), "use of closed network connection") {
	// 		return errdefs.DockerStreamCanceled(err)
	// 	}
	// 	return err
	// }

	// errChunk := &ErrorChunk{}
	// json.Unmarshal([]byte(lastChunk), errChunk)
	// if errChunk.Error != "" {
	// 	return errors.New(errChunk.Error)
	// }

	return nil
}

// TODO: handle errors from this goroutine
func (lm *LogManager) emitStream(logEntry *LogSubscription) error {
	if logEntry.Stream == nil {
		return nil
	}

	// Already watching logs, should just return
	logEntry.subscriptionStateMutex.Lock()
	if logEntry.Active {
		logEntry.subscriptionStateMutex.Unlock()
		return nil
	}
	logEntry.subscriptionStateMutex.Unlock()

	topic := lm.buildTopic(logEntry.ContainerName)
	scanner := bufio.NewScanner(logEntry.Stream)

	// cleanup func, closes the stream + saves the current logs in the database
	defer func() {
		safe.Go(func() {
			logEntry.subscriptionStateMutex.Lock()
			logEntry.Active = false
			logEntry.subscriptionStateMutex.Unlock()

			logEntry.Stream.Close()

			stage, appKey, appName, err := common.ParseContainerName(logEntry.ContainerName)
			if err != nil {
				return
			}

			logEntry.subscriptionStateMutex.Lock()
			logHistory := logEntry.logHistory

			err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), logEntriesToString(logHistory))
			if err != nil {
				logEntry.subscriptionStateMutex.Unlock()
				return
			}

			logEntry.subscriptionStateMutex.Unlock()

			safe.Go(func() {
				logEntry.subscriptionStateMutex.Lock()
				containerName := logEntry.ContainerName
				logEntry.subscriptionStateMutex.Unlock()

				logs, err := lm.getNonAgentLogs(containerName)
				if err != nil {
					return
				}

				for _, appLog := range logs {
					err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{appLog}, nil, nil)
					if err != nil {
						log.Error().Err(err).Msgf("failed to publish to %s in stream cleanup", topic)
					}
				}
			})

			log.Debug().Msgf("goroutine has finshed following logs for %s", logEntry.ContainerName)
		})
	}()

	logEntry.subscriptionStateMutex.Lock()
	logEntry.Active = true
	logEntry.subscriptionStateMutex.Unlock()

	var lastChunk string // if theres an error it will always be the last chunk of the stream
	for scanner.Scan() {
		chunk := scanner.Text()

		logEntry.subscriptionStateMutex.Lock()
		if len(logEntry.logHistory) == historyStorageLimit {
			logEntry.logHistory = logEntry.logHistory[1:]
		}

		logEntry.appendLog(LogEntry{entry: chunk, logType: CONTAINER})

		shouldPublish := logEntry.Publish

		logEntry.subscriptionStateMutex.Unlock()

		if shouldPublish {
			err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{chunk}, nil, nil)
			if err != nil {
				log.Error().Err(err).Msgf("failed to publish to %s in publish loop", topic)
			}
		}

		lastChunk = chunk
	}

	err := scanner.Err()
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return errdefs.DockerStreamCanceled(err)
		}
		return err
	}

	errChunk := &ErrorChunk{}
	json.Unmarshal([]byte(lastChunk), errChunk)
	if errChunk.Error != "" {
		return errors.New(errChunk.Error)
	}

	return nil
}

// ReviveDeadLogs will iterate over all apps that are running and check if it has an active logger subscription. If a subscription exists, it will publish the container logs.
func (lm *LogManager) ReviveDeadLogs() error {
	log.Info().Msg("Checking for alive log subscriptions")

	appStates, err := lm.AppStore.GetAllApps()
	if err != nil {
		return err
	}

	safe.Go(func() {
		for _, app := range appStates {
			containerName := common.BuildContainerName(app.Stage, uint64(app.AppKey), app.AppName)
			topic := lm.buildTopic(containerName)

			lm.activeLogsMutex.Lock()
			existingEntry := lm.activeLogs[containerName]
			lm.activeLogsMutex.Unlock()

			if existingEntry != nil {
				existingEntry.subscriptionStateMutex.Lock()
				if existingEntry.Active && existingEntry.Stream != nil {
					existingEntry.subscriptionStateMutex.Unlock()
					continue
				}
				existingEntry.subscriptionStateMutex.Unlock()
			}

			ctx := context.Background()
			result, err := lm.Messenger.Call(ctx, topics.MetaProcMatchSubscription, []interface{}{topic}, nil, nil, nil)
			if err != nil {
				log.Error().Err(err).Msg("failed to lookup subscription")
			}

			id := result.Arguments[0]
			reader, err := lm.getLogStream(containerName)
			if err != nil {
				if !errdefs.IsContainerNotFound(err) {
					log.Error().Err(err).Msg("failed to get log stream")
				}
			}

			subscriptionEntry := LogSubscription{
				ContainerName: containerName,
				logHistory:    make([]*LogEntry, 0),
				Stream:        reader,
				Active:        false,
				Publish:       false,
			}

			if id != nil {
				subscriptionEntry.SubscriptionID = fmt.Sprint(id)
				subscriptionEntry.Publish = true
			}

			lm.activeLogsMutex.Lock()
			lm.activeLogs[containerName] = &subscriptionEntry
			lm.activeLogsMutex.Unlock()

			if reader != nil {
				safe.Go(func() {
					lm.emitStream(&subscriptionEntry)
				})
			}
		}

	})

	return nil
}

func (lm *LogManager) getPersistedLogHistory(containerName string) ([]*LogEntry, error) {
	lm.activeLogsMutex.Lock()
	activeLogs := lm.activeLogs

	for _, logSession := range activeLogs {
		if logSession.ContainerName == containerName {
			logSession.subscriptionStateMutex.Lock()
			logHistory := logSession.logHistory
			logSession.subscriptionStateMutex.Unlock()

			if len(logHistory) > 0 {
				lm.activeLogsMutex.Unlock()
				return logHistory, nil
			}
		}
	}

	lm.activeLogsMutex.Unlock()

	stage, appKey, appName, err := common.ParseContainerName(containerName)
	if err != nil {
		return nil, err
	}

	app, err := lm.AppStore.GetApp(appKey, stage)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get app")
		return []*LogEntry{}, nil
	}

	if app == nil {
		return []*LogEntry{}, nil
	}

	// not found in memory, lets check database
	logs, err := lm.Database.GetAppLogHistory(appName, appKey, stage)
	if err != nil {
		log.Error().Err(err)
		if strings.Contains(err.Error(), "No logs found") {
			return []*LogEntry{}, nil
		}
	}

	var logEntries []*LogEntry
	for _, log := range logs {
		logEntries = append(logEntries, &LogEntry{entry: log, logType: CONTAINER})
	}

	return logEntries, nil
}

func (lm *LogManager) getNonAgentLogs(containerName string) ([]string, error) {
	history, err := lm.getPersistedLogHistory(containerName)
	if err != nil {
		return nil, err
	}

	containsOnlyAgentLogs := true
	for _, entry := range history {
		if entry.logType == CONTAINER {
			containsOnlyAgentLogs = false
			break
		}
	}

	if containsOnlyAgentLogs || len(history) == 0 {
		ctx := context.Background()
		options := common.Dict{"follow": false, "stdout": true, "stderr": true, "tail": "50"}
		reader, err := lm.Container.Logs(ctx, containerName, options)
		if err != nil {
			log.Warn().Err(err).Msgf("No log history found for: %s\n", containerName)
			return []string{}, nil
		}

		var containerHistory []string
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			containerHistory = append(containerHistory, scanner.Text())
		}

		err = reader.Close()
		if err != nil {
			log.Error().Err(err).Msg("failed to close reader after getting logs")
		}

		return containerHistory, nil
	}

	return []string{}, nil
}

func (lm *LogManager) GetLogHistory(containerName string) ([]string, error) {
	history, err := lm.getPersistedLogHistory(containerName)
	if err != nil {
		return nil, err
	}

	containsOnlyAgentLogs := true
	for _, entry := range history {
		if entry.logType == CONTAINER {
			containsOnlyAgentLogs = false
			break
		}
	}

	stringLogEntries := logEntriesToString(history)

	if containsOnlyAgentLogs || len(history) == 0 {
		ctx := context.Background()
		options := common.Dict{"follow": false, "stdout": true, "stderr": true, "tail": "50"}
		reader, err := lm.Container.Logs(ctx, containerName, options)
		if err != nil {
			log.Error().Err(err).Msg("Error occurred while trying to get log history when none were found")
			return stringLogEntries, nil
		}

		var containerHistory []string
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			containerHistory = append(containerHistory, scanner.Text())
		}

		err = reader.Close()
		if err != nil {
			log.Error().Err(err).Msg("failed to close reader after getting logs")
		}

		return append(stringLogEntries, containerHistory...), nil
	}

	return stringLogEntries, nil
}

func (lm *LogManager) SetupEndpoints() error {
	err := lm.Messenger.Subscribe(topics.MetaEventSubOnCreate, func(r messenger.Result) error {
		safe.Go(func() {
			_ = r.Arguments[0]                // the id of the client session that used to be listening
			subscriptionArg := r.Arguments[1] // the id of the subscription that was created
			subscription, ok := subscriptionArg.(map[string]interface{})
			if !ok {
				log.Error().Err(errors.New("failed to parse subscription args"))
			}

			uri := fmt.Sprint(subscription["uri"])
			id := fmt.Sprint(subscription["id"])

			// ignore any non log subscriptions
			if !strings.HasPrefix(uri, "reswarm.logs.") {
				return
			}

			topicSplit := strings.Split(uri, ".")
			serialNumber := topicSplit[2]
			containerName := topicSplit[3]

			// if the request is not for my device
			if serialNumber != lm.Container.GetConfig().ReswarmConfig.SerialNumber {
				return
			}

			lm.activeLogsMutex.Lock()
			activeLog := lm.activeLogs[containerName]
			lm.activeLogsMutex.Unlock()

			// there is at least one client subscribed, we should publish
			if activeLog != nil {

				activeLog.subscriptionStateMutex.Lock()
				activeLog.SubscriptionID = id // set the current subscriptionID
				activeLog.Publish = true
				activeLog.subscriptionStateMutex.Unlock()

				log.Debug().Msgf("Log Manager: A subscription was created, enabling publishing for %s", activeLog.ContainerName)
			} else {
				// we don't have an active stream yet, and also don't know the logtype
				// but we want to add an entry so we can populate the stream later on
				newActiveLog := LogSubscription{
					SubscriptionID: id,
					Publish:        true,
					logHistory:     make([]*LogEntry, 0),
					Active:         false,
					ContainerName:  containerName,
				}

				lm.activeLogsMutex.Lock()
				lm.activeLogs[containerName] = &newActiveLog
				lm.activeLogsMutex.Unlock()

				log.Debug().Msgf("Log Manager: A subscription was created without an active stream waiting for stream... %s\n", newActiveLog.ContainerName)
			}
		})

		return nil
	}, nil)

	if err != nil {
		return err
	}

	return lm.Messenger.Subscribe(topics.MetaEventSubOnDelete, func(r messenger.Result) error {
		safe.Go(func() {
			_ = r.Arguments[0]               // the id of the client session that used to be listening
			id := fmt.Sprint(r.Arguments[1]) // the id of the subscription that was deleted, in the delete we only receive the ID

			subscription := lm.GetLogTaskBySubscriptionID(id)

			if subscription != nil {
				// no clients are subscribed, should stop publishing
				subscription.subscriptionStateMutex.Lock()
				subscription.Publish = false
				subscription.subscriptionStateMutex.Unlock()

				log.Print("Log Manager: Stopped publishing logs for", subscription.ContainerName)
			}
		})

		return nil
	}, nil)
}

func (lm *LogManager) GetLogTaskBySubscriptionID(id string) *LogSubscription {
	lm.activeLogsMutex.Lock()
	defer lm.activeLogsMutex.Unlock()

	activeLogs := lm.activeLogs
	for _, subscription := range activeLogs {
		if subscription.SubscriptionID == id {
			return subscription
		}
	}
	return nil
}

func (lm *LogManager) initLogStream(containerName string, logType common.LogType, stream io.ReadCloser) error {
	lm.activeLogsMutex.Lock()
	exisitingLog := lm.activeLogs[containerName]
	lm.activeLogsMutex.Unlock()

	// found an entry without an active stream, populate the stream
	if exisitingLog != nil {
		exisitingLog.Stream = stream
		return lm.emitStream(exisitingLog)
	}

	// in case there is already an active subscription, we need to start publishing straight away
	id, err := lm.getActiveSubscriptionID(containerName)
	if err != nil {
		stream.Close()
		return err
	}

	activeLog := LogSubscription{
		ContainerName: containerName,
		logHistory:    make([]*LogEntry, 0),
		Stream:        stream,
		Active:        false,
		Publish:       false,
	}

	if id != "" {
		activeLog.Publish = true
		activeLog.SubscriptionID = id
	}

	lm.activeLogsMutex.Lock()
	lm.activeLogs[containerName] = &activeLog
	lm.activeLogsMutex.Unlock()
	return lm.emitStream(&activeLog)
}

func (lm *LogManager) getActiveSubscriptionID(containerName string) (string, error) {
	ctx := context.Background()
	result, err := lm.Messenger.Call(ctx, topics.MetaProcLookupSubscription, []interface{}{lm.buildTopic(containerName), common.Dict{"match": "wildcard"}}, nil, nil, nil)
	if err != nil {
		return "", err
	}

	if result.Arguments == nil {
		return "", nil
	}

	if len(result.Arguments) == 0 {
		return "", nil
	}

	id := result.Arguments[0]
	if id != nil {
		return fmt.Sprint(id), nil
	}

	return "", nil
}

func (lm *LogManager) getLogStream(containerName string) (io.ReadCloser, error) {
	options := common.Dict{"follow": true, "stdout": true, "stderr": true}

	ctx := context.Background()

	return lm.Container.Logs(ctx, containerName, options)
}

func (lm *LogManager) buildTopic(containerName string) string {
	serialNumber := lm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	return fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)
}

func (lm *LogManager) StreamBlockingChannel(containerName string, logType common.LogType, channel chan string) error {
	if channel != nil {
		return lm.initLogStreamChannel(containerName, logType, channel)
	}

	return errors.New("channel not found")
}

func (lm *LogManager) StreamChannel(containerName string, logType common.LogType, channel chan string) error {
	if channel != nil {
		safe.Go(func() {
			lm.initLogStreamChannel(containerName, logType, channel)
		})

		return nil
	}

	return errors.New("channel not found")
}

func (lm *LogManager) initLogStreamChannel(containerName string, logType common.LogType, channel chan string) error {
	lm.activeComposeLogsMutex.Lock()
	exisitingLog := lm.activeComposeLogs[containerName]
	lm.activeComposeLogsMutex.Unlock()

	// found an entry without an active stream, populate the stream
	if exisitingLog != nil {
		exisitingLog.ChannelStream = channel
		return lm.emitChannelStream(exisitingLog)
	}

	// in case there is already an active subscription, we need to start publishing straight away
	id, err := lm.getActiveSubscriptionID(containerName)
	if err != nil {
		return err
	}

	activeLog := LogSubscription{
		ContainerName: containerName,
		logHistory:    make([]*LogEntry, 0),
		ChannelStream: channel,
		Active:        false,
		Publish:       false,
	}

	if id != "" {
		activeLog.Publish = true
		activeLog.SubscriptionID = id
	}

	lm.activeComposeLogsMutex.Lock()
	lm.activeComposeLogs[containerName] = &activeLog
	lm.activeComposeLogsMutex.Unlock()

	return lm.emitChannelStream(&activeLog)
}

// StreamBlocking publishes a stream of string data to a specific subscribable container synchronisly.
func (lm *LogManager) StreamBlocking(containerName string, logType common.LogType, reader io.ReadCloser) error {
	if reader != nil {
		return lm.initLogStream(containerName, logType, reader)
	}
	return nil
}

// Stream publishes an stream of string data to a specific subscribable container
func (lm *LogManager) Stream(containerName string, logType common.LogType, otherReader io.ReadCloser) error {
	safe.Go(func() {
		reader, err := lm.getLogStream(containerName)
		if err != nil {
			if !errdefs.IsContainerNotFound(err) {
				return
			}
		}

		if reader != nil {
			safe.Go(func() {
				lm.initLogStream(containerName, logType, reader)
			})
			return
		}

		if otherReader != nil {
			safe.Go(func() {
				lm.initLogStream(containerName, logType, otherReader)
			})
		}
	})

	return nil
}

func (lm *LogManager) PublishProgress(containerName string, id string, status string, progress string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	entry := common.Dict{"id": id, "status": status, "progress": progress}
	err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{entry}, nil, nil)
	if err != nil {
		return err
	}
	return nil
}

// Publish publishes a message to a specific subscribable container.
func (lm *LogManager) Publish(containerName string, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	entry := common.Dict{"type": "build", "chunk": text}
	err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{entry}, nil, nil)
	if err != nil {
		return err
	}
	return nil
}

// Write adds an entry to the log history and publishes a message to a specific subscribable container.
func (lm *LogManager) Write(containerName string, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	logPayload := common.Dict{"type": "build", "chunk": text}

	lm.activeLogsMutex.Lock()
	activeLog := lm.activeLogs[containerName]

	if activeLog != nil {
		activeLog.subscriptionStateMutex.Lock()

		activeLog.appendLog(LogEntry{
			entry:   text,
			logType: AGENT,
		})

		activeLog.subscriptionStateMutex.Unlock()
	}

	lm.activeLogsMutex.Unlock()

	err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{logPayload}, nil, nil)
	if err != nil {
		return err
	}
	return nil
}
