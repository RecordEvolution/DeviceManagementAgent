package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/messenger"
)

func (ex *External) requestTerminalSessHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	if response.Arguments == nil {
		return nil, errors.New("no args found")
	}

	payloadArg := response.Arguments[0]
	payload, ok := payloadArg.(map[string]interface{})

	if !ok {
		return nil, errors.New("failed to parse payload")
	}

	containerNameKw := payload["containerName"]
	containerName, ok := containerNameKw.(string)
	if !ok {
		return nil, errors.New("failed to parse containerName")
	}

	termSess, err := ex.TerminalManager.RequestTerminalSession(containerName)
	if err != nil {
		return nil, err

	}

	// this will be used by the frontend to register/call to the terminal session
	responseData := common.Dict{
		"dataTopic":   termSess.DataTopic,
		"writeTopic":  termSess.WriteTopic,
		"resizeTopic": termSess.ResizeTopic,
		"sessionID":   termSess.SessionID,
	}

	return &messenger.InvokeResult{Arguments: []interface{}{responseData}}, nil
}
