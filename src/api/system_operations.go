package api

import (
	"context"
	"errors"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) systemRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to reboot device"))
	}

	err = ex.System.Reboot()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemShutdownHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to power off device"))
	}

	err = ex.System.Poweroff()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
