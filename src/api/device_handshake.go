package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"time"
)

func deviceHandshakeHandler(ex *External) func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{
				"utp": time.Now().Format(time.RFC3339),
				"id":  ex.Config.ReswarmConfig.SerialNumber,
			},
		}
	}
}
