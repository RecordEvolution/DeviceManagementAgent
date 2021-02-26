package api

import (
	"context"
	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/messenger/topics"
)

func (ex *External) updateReagent(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	progressCallback := func(increment uint64, currentBytes uint64, fileSize uint64) {
		progress := common.Dict{
			"increment":    increment,
			"currentBytes": currentBytes,
			"fileSize":     fileSize,
		}

		serialNumber := ex.Config.ReswarmConfig.SerialNumber
		topic := common.BuildAgentUpdateProgress(serialNumber)
		ex.Messenger.Publish(topics.Topic(topic), []interface{}{progress}, nil, nil)
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
