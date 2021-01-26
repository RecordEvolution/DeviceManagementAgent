package logging

import (
	"bufio"
	"fmt"
	"io"
	"reagent/api/common"
	"reagent/messenger"
	"strings"
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

func (lm *LogManager) Stream(containerName string, logType LogType, reader io.ReadCloser) error {
	serialNumber := lm.Messenger.GetConfig().SerialNumber
	topic := fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)
	fmt.Println(serialNumber, topic)

	scanner := bufio.NewScanner(reader)
	builder := strings.Builder{}
	for scanner.Scan() {
		chunk := scanner.Text()
		fmt.Println(chunk)
		builder.WriteString(chunk)

		args := []common.Dict{{"type": "build", "chunk": chunk}}

		err := lm.Messenger.Publish(topic, args, nil, nil)
		if err != nil {
			return err
		}
	}

	// TODO: store logs in db
	return nil
}

func (lm *LogManager) Write(containerName string, logType LogType, text string) error {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().SerialNumber, containerName)
	args := []common.Dict{{"type": "build", "chunk": text}}
	err := lm.Messenger.Publish(topic, args, nil, nil)
	if err != nil {
		return err
	}
	return nil
}
