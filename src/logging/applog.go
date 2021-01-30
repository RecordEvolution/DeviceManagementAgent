package logging

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reagent/api/common"
	"reagent/errdefs"
	"reagent/messenger"
	"time"
)

func GetBuildLogs(appid string) string {
	return "id"
}

func GetAppLogs(appid string) string {
	return "id"
}

type LogManager struct {
	Messenger messenger.Messenger
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
	Current    int64 `json:"current,omitempty"`
	Total      int64 `json:"total,omitempty"`
	Start      int64 `json:"start,omitempty"`
	// If true, don't show xB/yB
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
	Stream          string        `json:"stream,omitempty"`
	Status          string        `json:"status,omitempty"`
	Progress        *JSONProgress `json:"progressDetail,omitempty"`
	ProgressMessage string        `json:"progress,omitempty"` // deprecated
	ID              string        `json:"id,omitempty"`
	From            string        `json:"from,omitempty"`
	Time            int64         `json:"time,omitempty"`
	TimeNano        int64         `json:"timeNano,omitempty"`
	Error           *JSONError    `json:"errorDetail,omitempty"`
	ErrorMessage    string        `json:"error,omitempty"` // deprecated
	// Aux contains out-of-band data, such as digests for push signing and image id after building.
	Aux *json.RawMessage `json:"aux,omitempty"`
}

func (lm *LogManager) Stream(containerName string, logType LogType, reader io.ReadCloser) error {
	serialNumber := lm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	topic := fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)

	scanner := bufio.NewScanner(reader)
	// builder := strings.Builder{}

	messages := make([]JSONMessage, 0)
	for scanner.Scan() {
		chunk := scanner.Bytes()
		var message JSONMessage
		err := json.Unmarshal(chunk, &message)

		if err != nil {
			return err
		}

		args := []common.Dict{{"type": "build", "chunk": message}}

		err = lm.Messenger.Publish(topic, args, nil, nil)
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

	// TODO: store logs in db
	return nil
}

func (lm *LogManager) Write(containerName string, logType LogType, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().ReswarmConfig.SerialNumber, containerName)
	args := []common.Dict{{"type": "build", "chunk": text}}
	err := lm.Messenger.Publish(topic, args, nil, nil)
	if err != nil {
		return err
	}
	return nil
}
