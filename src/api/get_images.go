package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) getImagesHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	result, err := ex.Container.ListImages(ctx, nil)
	if err != nil {
		return nil, err
	}

	// See https://github.com/golang/go/wiki/InterfaceSlice
	images := make([]interface{}, 0)
	for _, image := range result {
		images = append(images, image)
	}

	return &messenger.InvokeResult{
		Arguments: images,
	}, nil
}
