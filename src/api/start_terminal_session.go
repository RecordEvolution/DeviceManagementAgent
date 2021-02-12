package api

import (
	"context"
	"errors"
	"reagent/messenger"
)

func (ex *External) startTerminalSessHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
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

	registrationIDKw := payload["registrationID"]
	registrationID, ok := registrationIDKw.(uint64)
	if !ok {
		return nil, errors.New("failed to parse registrationID")
	}

	err := ex.TerminalManager.StartTerminalSession(sessionID, registrationID)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
