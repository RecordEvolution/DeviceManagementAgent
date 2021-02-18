package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) getImagesHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	images, err := ex.Container.ListImages(ctx, nil)

	if err != nil {
		return nil, err
	}

	// See https://github.com/golang/go/wiki/InterfaceSlice
	args := make([]interface{}, 0)
	for _, image := range images {
		args = append(args, image)
	}

	return &messenger.InvokeResult{}, nil
}
