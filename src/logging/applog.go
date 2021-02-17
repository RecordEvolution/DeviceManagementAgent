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
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

func GetBuildLogs(appid string) string {
	return "id"
}

func GetAppLogs(appid string) string {
	return "id"
}

type LogSubscription struct {
	ContainerName string
	Stream        io.ReadCloser
	Streaming     bool
}

type LogManager struct {
	Container  container.Container
	Messenger  messenger.Messenger
	activeLogs map[string]*LogSubscription
	mapMutex   sync.Mutex
}

type LogType string

const (
	PULL    LogType = "PULL"
	PUSH    LogType = "PUSH"
	BUILD   LogType = "BUILD"
	RUNNING LogType = "RUNNING"
)

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

// TODO: handle errors from this goroutine
func (lm *LogManager) emitStream(subscription *LogSubscription) {
	topic := lm.buildTopic(subscription.ContainerName)

	// shouldn't occur, but for safety reasons...
	if subscription.Streaming {
		return
	}

	scanner := bufio.NewScanner(subscription.Stream)
	subscription.Streaming = true
	defer subscription.Stream.Close()

	for scanner.Scan() {
		chunk := scanner.Text()

		err := lm.Messenger.Publish(topics.Topic(topic), []interface{}{chunk}, nil, nil)
		if err != nil {
			return
		}

	}

	log.Print("goroutine has finshed publishing logs for", subscription.ContainerName)

	subscription.Streaming = false
}

// ReviveDeadLogs will iterate over all apps that are running and check if it has an active logger subscription. If a subscription exists, it will publish the container logs.
func (lm *LogManager) ReviveDeadLogs(appStates []*common.App) error {
	log.Info().Msg("Checking for alive log subscriptions")

	for _, app := range appStates {
		containerName := common.BuildContainerName(app.Stage, uint64(app.AppKey), app.AppName)
		topic := lm.buildTopic(containerName)

		ctx := context.Background()
		result, err := lm.Messenger.Call(ctx, topics.MetaProcLookupSubscription, []interface{}{topic, common.Dict{"match": "wildcard"}}, nil, nil, nil)
		if err != nil {
			return err
		}

		id := result.Arguments[0]
		if id == nil {
			log.Debug().Msgf("(%s) app %s has no active subs.. skipping..", app.Stage, app.AppName)
			continue
		}

		// app is running and there's a subscription active in this realm, init publish
		lm.createLogTask(fmt.Sprint(id), containerName)
	}
	return nil
}

func (lm *LogManager) Init() error {
	lm.activeLogs = make(map[string]*LogSubscription)

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

		idString := fmt.Sprint(id)
		lm.mapMutex.Lock()
		if lm.activeLogs[idString] != nil {
			lm.mapMutex.Unlock()
			// this shouldn't happen if the subscriptions are properly removed
			return errors.New("The subscription somehow already exists, this should never happen")
		}
		lm.mapMutex.Unlock()

		lm.createLogTask(idString, containerName)

		return nil
	}, nil)

	err = lm.Messenger.Subscribe(topics.MetaEventSubOnDelete, func(r messenger.Result) error {
		_ = r.Arguments[0]   // the id of the client session that used to be listening
		id := r.Arguments[1] // the id of the subscription that was deleted, in the delete we only receive the ID
		idString := fmt.Sprint(id)

		lm.mapMutex.Lock()
		defer lm.mapMutex.Unlock()
		if lm.activeLogs[idString] == nil {
			return nil
		}

		activeSubscription := lm.activeLogs[idString]

		if activeSubscription.Stream == nil {
			log.Info().Msg("stream was empty, nothing to close")
		} else {
			// cancel the io stream that is active
			// TODO: figure out way to handle errors inside a subscription callback, however this error would be rare
			err := activeSubscription.Stream.Close()
			if err != nil {
				return errors.New("error occured while trying to close stream")
			}

			log.Print("Closed active stream for", activeSubscription.ContainerName)
		}

		// remove entry from active logs map
		delete(lm.activeLogs, idString)

		return nil
	}, nil)

	if err != nil {
		return err
	}
	return nil
}

func (lm *LogManager) createLogTask(id string, containerName string) error {
	stream, err := lm.getLogStream(containerName)
	if err != nil {
		// It's possible the container does not exist yet
		// e.g. if we trigger a subscription via an app build
		// in that case we will just keep the session in memory and trigger it later
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	}

	subscriptionEntry := LogSubscription{
		ContainerName: containerName,
		Stream:        stream,
		Streaming:     false,
	}

	lm.mapMutex.Lock()
	lm.activeLogs[id] = &subscriptionEntry
	lm.mapMutex.Unlock()

	// a subscription can be created without an actual stream, in that case don't stream
	if stream == nil {
		return nil
	}

	go lm.emitStream(&subscriptionEntry)

	return nil
}

func (lm *LogManager) getLogStream(containerName string) (io.ReadCloser, error) {
	options := common.Dict{"follow": true, "stdout": true, "stderr": true, "tail": "100"}

	ctx := context.Background()

	return lm.Container.Logs(ctx, containerName, options)
}

func (lm *LogManager) buildTopic(containerName string) string {
	serialNumber := lm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	return fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)
}

func (lm *LogManager) UpdateLogStream(containerName string) error {
	for _, subscription := range lm.activeLogs {
		if subscription.ContainerName == containerName && !subscription.Streaming {
			reader, err := lm.getLogStream(containerName)
			if err != nil {
				return err
			}

			if reader != nil {
				subscription.Stream = reader
				go lm.emitStream(subscription)
			}
		}
	}
	return nil
}

// Stream meant to be used for pull / build streams or any stream that isn't a container logs stream
func (lm *LogManager) Stream(containerName string, logType LogType, reader io.ReadCloser) error {
	topic := lm.buildTopic(containerName)
	scanner := bufio.NewScanner(reader)

	messages := make([]JSONMessage, 0)
	for scanner.Scan() {
		chunk := scanner.Bytes()
		var message JSONMessage
		err := json.Unmarshal(chunk, &message)

		if err != nil {
			return err
		}

		entry := common.Dict{"type": "build", "chunk": message}
		args := make([]interface{}, 0)
		args = append(args, entry)

		err = lm.Messenger.Publish(topics.Topic(topic), args, nil, nil)
		if err != nil {
			return err
		}

		// TODO: show proper errors to user instead of actual docker api error
		if message.Error != nil {
			msg := message.Error.Message
			if message.Error.Message == "" && message.ErrorMessage != "" {
				msg = message.ErrorMessage
			}
			if msg == "" {
				msg = "an error occured during the docker build"
			}
			return errdefs.BuildFailed(errors.New(msg))
		}

		messages = append(messages, message)
	}

	err := reader.Close()
	if err != nil {
		return err
	}

	err = scanner.Err()
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return errdefs.DockerBuildCanceled(err)
		}
		return err
	}

	// TODO: store build/pull logs in db
	return nil
}

func (lm *LogManager) Write(containerName string, logType LogType, text string) error {
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
