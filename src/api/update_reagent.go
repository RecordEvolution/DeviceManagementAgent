package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) updateReagent(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	updateResult, err := ex.System.UpdateIfRequired()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{Arguments: []interface{}{updateResult}}, nil
}
