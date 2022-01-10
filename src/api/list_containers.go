package api

import (
	"context"
	"errors"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) listContainersHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to list containers"))
	}

	containers, err := ex.Container.GetContainers(ctx)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{Arguments: []interface{}{containers}}, nil
}
