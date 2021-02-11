package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"time"
)

func (ex *External) deviceHandshakeHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	kwargs := common.Dict{
		"utp": time.Now().Format(time.RFC3339),
		"id":  ex.Config.ReswarmConfig.SerialNumber,
	}

	return &messenger.InvokeResult{ArgumentsKw: kwargs}, nil
}
