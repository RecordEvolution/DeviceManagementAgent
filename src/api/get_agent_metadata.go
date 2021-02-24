package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"runtime"
)

func (ex *External) getAgentMetadataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	dict := common.Dict{
		"oos":     runtime.GOOS,
		"arch":    runtime.GOARCH,
		"version": ex.System.GetVersion(),
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
