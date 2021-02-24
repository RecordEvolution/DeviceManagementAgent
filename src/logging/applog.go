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
	"reagent/store"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type ActiveLog struct {
	SubscriptionID string // The currently active subscriptionID for this log entry, can be empty
	ContainerName  string
	Stream         io.ReadCloser
	logHistory     []string
	LogType        common.LogType
	Publish        bool
	Active         bool
	StateLock      sync.Mutex
}

type LogManager struct {
	Container  container.Container
	Messenger  messenger.Messenger
	Database   persistence.Database
	AppStore   store.AppStore
	activeLogs map[string]*ActiveLog
	mapMutex   sync.Mutex
}

// Amount of lines that will be stored for each app
const historyStorageLimit = 200

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

func (lm *LogManager) ClearLogHistory(containerName string) error {
	lm.mapMutex.Lock()
	activeLogEntry := lm.activeLogs[containerName]
	lm.mapMutex.Unlock()

	if activeLogEntry == nil {
		return nil
	}

	// clear locally
	activeLogEntry.StateLock.Lock()
	activeLogEntry.logHistory = make([]string, 0)
	activeLogEntry.StateLock.Unlock()

	stage, appKey, appName, err := common.ParseContainerName(containerName)
	if err != nil {
		return err
	}

	// clear in database
	err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), common.APP, []string{})
	if err != nil {
		return err
	}

	err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), common.BUILD, []string{})
	if err != nil {
		return err
	}

	err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), common.PULL, []string{})
	if err != nil {
		return err
	}

	err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), common.PUSH, []string{})
	if err != nil {
		return err
	}

	return nil
}

// TODO: handle errors from this goroutine
func (lm *LogManager) emitStream(logEntry *ActiveLog) error {
	if logEntry.Stream == nil {
		return nil
	}

	// Already watching logs, should just return
	logEntry.StateLock.Lock()
	active := logEntry.Active
	logEntry.StateLock.Unlock()

	if active {
		return nil
	}

	topic := lm.buildTopic(logEntry.ContainerName)
	scanner := bufio.NewScanner(logEntry.Stream)

	// clear old log history
	logEntry.StateLock.Lock()
	logEntry.logHistory = make([]string, 0)
	logEntry.StateLock.Unlock()

	// cleanup func, closes the stream + saves the current logs in the database
	defer func() {
		go func() {
			logEntry.StateLock.Lock()
			logEntry.Active = false
			logEntry.StateLock.Unlock()

			err := logEntry.Stream.Close()
			if err != nil {
				return
			}

			stage, appKey, appName, err := common.ParseContainerName(logEntry.ContainerName)
			if err != nil {
				return
			}

			logEntry.StateLock.Lock()
			logType := logEntry.LogType
			logHistory := logEntry.logHistory
			logEntry.StateLock.Unlock()

			err = lm.Database.UpsertLogHistory(appName, appKey, common.Stage(stage), logType, logHistory)
			if err != nil {
				return
			}

			log.Print("goroutine has finshed following logs for", logEntry.ContainerName)
		}()
	}()

	logEntry.StateLock.Lock()
	logEntry.Active = true
	logEntry.StateLock.Unlock()
	for scanner.Scan() {
		chunk := scanner.Text()

		logEntry.StateLock.Lock()
		if len(logEntry.logHistory) == historyStorageLimit {
			logEntry.logHistory = logEntry.logHistory[1:]
		}

		logEntry.logHistory = append(logEntry.logHistory, chunk)

		shouldPublish := logEntry.Publish
		logEntry.StateLock.Unlock()

		if shouldPublish {
			lm.Messenger.Publish(topics.Topic(topic), []interface{}{chunk}, nil, nil)
		}
	}

	err := scanner.Err()
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return errdefs.DockerStreamCanceled(err)
		}
		return err
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

	for _, app := range appStates {
		containerName := common.BuildContainerName(app.Stage, uint64(app.AppKey), app.AppName)
		topic := lm.buildTopic(containerName)

		ctx := context.Background()
		result, err := lm.Messenger.Call(ctx, topics.MetaProcLookupSubscription, []interface{}{topic, common.Dict{"match": "wildcard"}}, nil, nil, nil)
		if err != nil {
			return err
		}

		id := result.Arguments[0]

		app.StateLock.Lock()
		curAppState := app.CurrentState
		app.StateLock.Unlock()

		logType := common.GetCurrentLogType(curAppState)
		reader, err := lm.getLogStream(containerName)
		if err != nil {
			if !errdefs.IsContainerNotFound(err) {
				return err
			}
		}

		subscriptionEntry := ActiveLog{
			ContainerName: containerName,
			logHistory:    make([]string, 0),
			LogType:       logType,
			Stream:        reader,
			Active:        false,
			Publish:       true,
		}

		if id != nil {
			subscriptionEntry.SubscriptionID = fmt.Sprint(id)
		}

		lm.mapMutex.Lock()
		lm.activeLogs[containerName] = &subscriptionEntry
		lm.mapMutex.Unlock()

		if reader != nil {
			go lm.emitStream(&subscriptionEntry)
		}
	}
	return nil
}

func (lm *LogManager) GetLogHistory(appName string, appKey uint64, stage common.Stage, logType common.LogType) ([]string, error) {
	for _, logSession := range lm.activeLogs {
		containerName := common.BuildContainerName(stage, appKey, appName)
		if logSession.ContainerName == containerName {
			return logSession.logHistory, nil
		}
	}

	// not found in memory, lets check database
	logs, err := lm.Database.GetAppLogHistory(appName, appKey, stage, logType)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

func (lm *LogManager) getLogHistoryByContainerName(containerName string) ([]string, error) {
	lm.mapMutex.Lock()
	activeLogs := lm.activeLogs
	lm.mapMutex.Unlock()

	for _, logSession := range activeLogs {
		if logSession.ContainerName == containerName {

			logSession.StateLock.Lock()
			logHistory := logSession.logHistory
			logSession.StateLock.Unlock()

			if len(logHistory) > 0 {
				return logHistory, nil
			}
		}
	}

	stage, appKey, appName, err := common.ParseContainerName(containerName)

	app, err := lm.AppStore.GetApp(appKey, stage)
	if err != nil {
		return nil, err
	}

	if app == nil {
		return []string{}, nil
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	logType := common.GetCurrentLogType(curAppState)

	// not found in memory, lets check database
	logs, err := lm.Database.GetAppLogHistory(appName, appKey, stage, logType)
	if err != nil {
		if strings.Contains(err.Error(), "No logs found") {
			return []string{}, nil
		}
	}

	return logs, nil
}

func (lm *LogManager) SetupEndpoints() error {
	lm.activeLogs = make(map[string]*ActiveLog)

	_ = lm.Messenger.Subscribe(topics.MetaEventSubOnSubscribe, func(r messenger.Result) error {
		subscriptionID := fmt.Sprint(r.Arguments[1]) // the id of the subscription that was created

		activeLog := lm.getLogTaskBySubscriptionID(subscriptionID)
		if activeLog == nil {
			return nil
		}

		history, err := lm.getLogHistoryByContainerName(activeLog.ContainerName)
		if err != nil {
			return err
		}

		for _, logEntry := range history {
			err := lm.Publish(activeLog.ContainerName, logEntry)
			if err != nil {
				return err
			}
		}

		return nil
	}, nil)

	err := lm.Messenger.Subscribe(topics.MetaEventSubOnCreate, func(r messenger.Result) error {
		_ = r.Arguments[0]                // the id of the client session that used to be listening
		subscriptionArg := r.Arguments[1] // the id of the subscription that was created
		subscription, ok := subscriptionArg.(map[string]interface{})
		if !ok {
			return errors.New("failed to parse subscription args")
		}

		uri := fmt.Sprint(subscription["uri"])
		id := fmt.Sprint(subscription["id"])

		// ignore any non log subscriptions
		if !strings.HasPrefix(uri, "reswarm.logs.") {
			return nil
		}

		topicSplit := strings.Split(uri, ".")
		serialNumber := topicSplit[2]
		containerName := topicSplit[3]

		// if the request is not for my device
		if serialNumber != lm.Container.GetConfig().ReswarmConfig.SerialNumber {
			return nil
		}

		lm.mapMutex.Lock()
		activeLog := lm.activeLogs[containerName]
		lm.mapMutex.Unlock()

		// there is at least one client subscribed, we should publish
		if activeLog != nil {

			activeLog.StateLock.Lock()
			activeLog.SubscriptionID = id // set the current subscriptionID
			activeLog.Publish = true
			// active := activeLog.Active
			activeLog.StateLock.Unlock()

			// ensure we are actually actively streaming
			// if active && activeLog.LogType == common.APP {
			// 	log.Debug().Msgf("Log Manager: we weren't active yet for %s", activeLog.ContainerName)
			// 	return lm.Stream(containerName, activeLog.LogType, nil)
			// }

			log.Debug().Msgf("Log Manager: A subscription was created, enabling publishing for %s", activeLog.ContainerName)
		} else {
			// we don't have an active stream yet, and also don't know the logtype
			// but we want to add an entry so we can populate the stream later on
			newActiveLog := ActiveLog{
				SubscriptionID: id,
				Publish:        true,
				logHistory:     make([]string, 0),
				Active:         false,
				ContainerName:  containerName,
			}

			lm.mapMutex.Lock()
			lm.activeLogs[containerName] = &newActiveLog
			lm.mapMutex.Unlock()

			log.Debug().Msgf("Log Manager: A subscription was created without an active stream waiting for stream...", newActiveLog.ContainerName)
		}

		return nil
	}, nil)

	if err != nil {
		return err
	}

	return lm.Messenger.Subscribe(topics.MetaEventSubOnDelete, func(r messenger.Result) error {
		_ = r.Arguments[0]               // the id of the client session that used to be listening
		id := fmt.Sprint(r.Arguments[1]) // the id of the subscription that was deleted, in the delete we only receive the ID

		subscription := lm.getLogTaskBySubscriptionID(id)

		if subscription != nil {
			// no clients are subscribed, should stop publishing
			subscription.StateLock.Lock()
			subscription.Publish = false
			subscription.StateLock.Unlock()

			log.Print("Log Manager: Stopped publishing logs for", subscription.ContainerName)
		}

		return nil
	}, nil)
}

func (lm *LogManager) getLogTaskBySubscriptionID(id string) *ActiveLog {
	lm.mapMutex.Lock()
	activeLogs := lm.activeLogs
	lm.mapMutex.Unlock()

	for _, subscription := range activeLogs {
		if subscription.SubscriptionID == id {
			return subscription
		}
	}
	return nil
}

func (lm *LogManager) initLogStream(containerName string, logType common.LogType, stream io.ReadCloser) error {
	lm.mapMutex.Lock()
	exisitingLog := lm.activeLogs[containerName]
	lm.mapMutex.Unlock()

	// found an entry without an active stream, populate the stream
	if exisitingLog != nil {
		exisitingLog.LogType = logType
		exisitingLog.Stream = stream
		return lm.emitStream(exisitingLog)
	}

	// in case there is already an active subscription, we need to start publishing straight away
	id, err := lm.getActiveSubscriptionID(containerName)
	if err != nil {
		return err
	}

	activeLog := ActiveLog{
		ContainerName: containerName,
		logHistory:    make([]string, 0),
		LogType:       logType,
		Stream:        stream,
		Active:        false,
		Publish:       false,
	}

	if id != "" {
		activeLog.Publish = true
		activeLog.SubscriptionID = id
	}

	lm.mapMutex.Lock()
	lm.activeLogs[containerName] = &activeLog
	lm.mapMutex.Unlock()

	return lm.emitStream(&activeLog)
}

func (lm *LogManager) getActiveSubscriptionID(containerName string) (string, error) {
	ctx := context.Background()
	result, err := lm.Messenger.Call(ctx, topics.MetaProcLookupSubscription, []interface{}{lm.buildTopic(containerName), common.Dict{"match": "wildcard"}}, nil, nil, nil)
	if err != nil {
		return "", err
	}

	id := result.Arguments[0]
	if id != nil {
		return fmt.Sprint(id), nil
	}

	return "", nil
}

func (lm *LogManager) getLogStream(containerName string) (io.ReadCloser, error) {
	// start from last log, do not get any history
	options := common.Dict{"follow": true, "stdout": true, "stderr": true, "tail": "0"}

	ctx := context.Background()

	return lm.Container.Logs(ctx, containerName, options)
}

func (lm *LogManager) buildTopic(containerName string) string {
	serialNumber := lm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	return fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)
}

// StreamBlocking publishes an stream of string data to a specific subscribable container synchronisly.
func (lm *LogManager) StreamBlocking(containerName string, logType common.LogType, reader io.ReadCloser) error {
	if reader != nil {
		return lm.initLogStream(containerName, logType, reader)
	}
	return nil
}

// Stream publishes an stream of string data to a specific subscribable container
func (lm *LogManager) Stream(containerName string, logType common.LogType, otherReader io.ReadCloser) error {
	reader, err := lm.getLogStream(containerName)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	}

	if reader != nil {
		go lm.initLogStream(containerName, logType, reader)
		return nil
	}

	if otherReader != nil {
		go lm.initLogStream(containerName, logType, otherReader)
	}

	return nil
}

// Publish publishes a message to a specific subscribable container.
func (lm *LogManager) Publish(containerName string, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	entry := common.Dict{"type": "build", "chunk": text}
	args := make([]interface{}, 0)
	args = append(args, entry)

	err := lm.Messenger.Publish(topics.Topic(topic), args, nil, nil)
	if err != nil {
		return err
	}
	return nil
}

// Write adds an entry to the log history and publishes a message to a specific subscribable container.
func (lm *LogManager) Write(containerName string, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	entry := common.Dict{"type": "build", "chunk": text}
	args := make([]interface{}, 0)
	args = append(args, entry)

	lm.mapMutex.Lock()
	activeLog := lm.activeLogs[containerName]
	lm.mapMutex.Unlock()

	if activeLog != nil {
		activeLog.StateLock.Lock()
		activeLog.logHistory = append(activeLog.logHistory, text)
		activeLog.StateLock.Unlock()
	}

	err := lm.Messenger.Publish(topics.Topic(topic), args, nil, nil)
	if err != nil {
		return err
	}
	return nil
}
