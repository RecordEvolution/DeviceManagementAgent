package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) getTunnelState(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	state, err := ex.TunnelManager.GetState()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{state},
	}, nil
}
