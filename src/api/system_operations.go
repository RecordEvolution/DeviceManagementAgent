package api

import (
	"context"
	// "reagent/common"
	"reagent/messenger"
	"reagent/system"
)

func (ex *External) systemRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := system.Reboot()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemShutdownHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := system.Poweroff()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
