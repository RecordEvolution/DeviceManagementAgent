package api

import (
	"context"
	// "reagent/common"
	"reagent/messenger"
	"reagent/system"
)

func (ex *External) systemRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	system.Reboot()

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemShutdownHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	system.Poweroff()

	return &messenger.InvokeResult{}, nil
}
