package api

import (
	"context"
	"errors"
	"reagent/messenger"
)

func (ex *External) stopTerminalSession(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	if response.Arguments == nil {
		return nil, errors.New("failed to parse args, payload is missing")
	}

	payloadArg := response.Arguments[0]
	payload, ok := payloadArg.(map[string]interface{})

	if !ok {
		return nil, errors.New("failed to parse payload")
	}

	sessionIDKw := payload["sessionID"]
	sessionID, ok := sessionIDKw.(string)
	if !ok {
		return nil, errors.New("failed to parse sessionID")
	}

	err := ex.TerminalManager.StopTerminalSession(sessionID)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
