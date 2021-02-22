package api

import (
	"context"
	"errors"
	"reagent/messenger"
	"reagent/system"
)

func (ex *External) updateReagent(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	if response.Arguments == nil {
		return nil, errors.New("failed to parse args, payload is missing")
	}

	versionString, ok := response.Arguments[0].(string)
	if !ok {
		return nil, errors.New("failed to parse version string argument, invalid type")
	}

	err := system.GetNewAgent(versionString)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
