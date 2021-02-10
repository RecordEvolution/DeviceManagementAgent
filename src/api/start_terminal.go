package api

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/messenger"

	"github.com/gammazero/nexus/v3/wamp"
)

func (ex *External) startTerminalHandler(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	payloadArg := response.Arguments[0]

	payload, ok := payloadArg.(map[string]interface{})

	if !ok {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": "failed to parse payload"},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	containerNameKw := payload["containerName"]
	containerName, ok := containerNameKw.(string)
	if !ok {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": "failed to parse containerName"},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	fmt.Println("starting terminal manager")
	err := ex.TerminalManager.Start(containerName)

	if err != nil {
		return messenger.InvokeResult{
			ArgumentsKw: common.Dict{"cause": err.Error()},
			Err:         string(wamp.ErrInvalidArgument),
		}
	}

	return messenger.InvokeResult{}
}
