package api

import (
	"context"
	"errors"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
)

func (ex *External) getStorageDataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to get storage data"))
	}

	diskUsageEntry, err := filesystem.AvailableDiskSpace()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{diskUsageEntry},
	}, nil
}
