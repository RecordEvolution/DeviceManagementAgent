package api

import (
	"context"
	"reagent/messenger"
)

func (ex *External) systemRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := ex.System.Reboot()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemShutdownHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := ex.System.Poweroff()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
