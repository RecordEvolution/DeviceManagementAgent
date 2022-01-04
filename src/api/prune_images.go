// prune_images

package api

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/messenger"
)

func (ex *External) pruneImageHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	args := response.Arguments
	options := common.Dict{}

	if args != nil || args[0] != nil {
		argsDict, ok := args[0].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("first param should be a dict")
		}

		options = argsDict
	}

	if options["all"] != nil {
		all, ok := options["all"].(bool)
		if !ok {
			return nil, errors.New("all value for container prune is not a boolean")
		}

		if all {
			output, err := ex.Container.PruneSystem(ctx)
			if err != nil {
				return nil, err
			}

			return &messenger.InvokeResult{Arguments: []interface{}{string(output)}}, nil
		}
	}

	err := ex.Container.PruneImages(context.Background(), options)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
