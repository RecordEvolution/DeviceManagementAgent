package logging

import (
	"bufio"
	"fmt"
	"io"
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

func (lm *LogManager) Broadcast(containerName string, logType LogType, reader io.ReadCloser) {
	topic := fmt.Sprintf("reswarm.logs.%s.%s", lm.Messenger.GetConfig().SerialNumber, containerName)

	scanner := bufio.NewScanner(reader)
	builder := strings.Builder{}
	fmt.Println()
	fmt.Println()
	for scanner.Scan() {
		chunk := scanner.Text()
		fmt.Println(chunk)
		builder.WriteString(chunk)
		args := []messenger.Dict{{"type": "build", "chunk": chunk}}
		lm.Messenger.Publish(topic, args, nil, nil)
	}

	// TODO: store logs in db
}
