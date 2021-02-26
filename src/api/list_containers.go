package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) listContainersHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	containers, err := ex.Container.GetContainers(ctx)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{Arguments: []interface{}{containers}}, nil
}
