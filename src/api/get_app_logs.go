package api

import (
	"context"
	"errors"
	"fmt"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) getAppLogHistoryHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to get app log history"))
	}

	args := response.Arguments

	if args == nil || args[0] == nil {
		return nil, fmt.Errorf("arguments are missing")
	}

	argsDict, ok := args[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("first param should be a dict")
	}

	containerName, ok := argsDict["containerName"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid value for containerName")
	}

	history, err := ex.LogManager.GetLogHistory(containerName)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{history},
	}, nil
}
