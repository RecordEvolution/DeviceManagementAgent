package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/system"
	"strings"
)

func (ex *External) updateReagent(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to update reagent"))
	}

	osVersion, err := system.GetOSVersion()
	if err != nil {
		return nil, err
	}

	if !strings.Contains(osVersion, "ReswarmOS") {
		return nil, errors.New("cannot update the agent on a not ReswarmOS system")
	}

	progressCallback := func(increment uint64, currentBytes uint64, fileSize uint64) {
		progress := common.Dict{
			"increment":    increment,
			"currentBytes": currentBytes,
			"fileSize":     fileSize,
		}

		serialNumber := ex.Config.ReswarmConfig.SerialNumber
		topic := common.BuildAgentUpdateProgress(serialNumber)
		ex.LogMessenger.Publish(topics.Topic(topic), []interface{}{progress}, nil, nil)
	}

	updateResult, err := ex.System.UpdateIfRequired(progressCallback)
	if err != nil {
		if !errdefs.IsInProgress(err) {
			return nil, err
		}
		return &messenger.InvokeResult{Arguments: []interface{}{updateResult}}, nil
	}

	return &messenger.InvokeResult{Arguments: []interface{}{updateResult}}, nil
}
