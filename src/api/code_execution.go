package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
	"time"
)

func (ex *External) codeExecutionHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	args := response.Arguments
	if args == nil || args[0] == nil {
		return nil, errors.New("args array should not be empty")
	}

	argsDict, ok := args[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("first param should be a dict")
	}

	cmdName, ok := argsDict["cmd"].(string)
	if !ok {
		return nil, fmt.Errorf("cmd param should be a string")
	}

	cmdArgsInterface, ok := argsDict["args"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("args param should be an array of primitive values")
	}

	commandTimeout := uint64(1000)
	if argsDict["timeout"] != nil {
		commandTimeout, ok = argsDict["timeout"].(uint64)
		if !ok {
			return nil, fmt.Errorf("the timeout param should be an uint64")
		}
	}

	cmdArgs := make([]string, len(cmdArgsInterface))
	for _, arg := range cmdArgsInterface {
		cmdArgs = append(cmdArgs, fmt.Sprint(arg))
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	go func() {
		time.Sleep(time.Millisecond * time.Duration(commandTimeout))
		scanner := bufio.NewScanner(cmdStdout)
		for scanner.Scan() {
			output := scanner.Text()
			topicAffix := fmt.Sprintf("%s_%d", topics.CmdExecutionPrefix, cmd.Process.Pid)
			topic := common.BuildExternalApiTopic(ex.Config.ReswarmConfig.SerialNumber, topicAffix)

			ex.Messenger.Publish(topics.Topic(topic), []interface{}{output}, nil, nil)
		}
	}()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("command failed to run: %s", err.Error())
	}

	respArgs := []interface{}{common.Dict{"pid": cmd.Process.Pid}}

	return &messenger.InvokeResult{Arguments: respArgs}, nil
}