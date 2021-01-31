package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"

	"github.com/gammazero/nexus/v3/wamp"
)

func (ex *External) getImagesHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	images, err := ex.StateMachine.Container.ListImages(ctx, nil)

	if err != nil {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			// TODO: show different URI error based on error that was returned upwards
			Err: string(wamp.ErrInvalidArgument),
		}
	}

	// See https://github.com/golang/go/wiki/InterfaceSlice
	args := make([]interface{}, 0)
	for _, image := range images {
		args = append(args, image)
	}

	return messenger.InvokeResult{Arguments: args}
}
