// prune_images

package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) pruneImageHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := ex.Container.PruneImages(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
