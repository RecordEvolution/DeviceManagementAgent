package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) startTerminalSessHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("DEVELOP", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to start a terminal session"))
	}

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

	err = ex.TerminalManager.StartTerminalSession(sessionID, registrationID)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) stopTerminalSession(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("DEVELOP", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to start a terminal session"))
	}

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

	err = ex.TerminalManager.StopTerminalSession(sessionID)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) requestTerminalSessHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("DEVELOP", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to start a terminal session"))
	}

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
