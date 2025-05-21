package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/messenger/topics"
)

func (ex *External) updateReagent(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to update reagent"))
	}

	// reswarmModeEnabled, _ := filesystem.PathExists("/opt/reagent/reswarm-mode")
	// if !reswarmModeEnabled {
	// 	return nil, errors.New("cannot update on non reswarm-mode enabled system")
	// }

	progressCallback := func(downloadProgress filesystem.DownloadProgress) {
		progress := common.Dict{
			"increment":    downloadProgress.Increment,
			"currentBytes": downloadProgress.CurrentBytes,
			"fileSize":     downloadProgress.TotalFileSize,
		}

		serialNumber := ex.Config.ReswarmConfig.SerialNumber
		topic := common.BuildAgentUpdateProgress(serialNumber)
		ex.LogMessenger.Publish(topics.Topic(topic), []interface{}{progress}, nil, nil)
	}

	updateResult, err := ex.System.UpdateSystem(progressCallback, true)
	if err != nil {
		if !errdefs.IsInProgress(err) {
			return nil, err
		}
		return &messenger.InvokeResult{Arguments: []interface{}{updateResult}}, nil
	}

	return &messenger.InvokeResult{Arguments: []interface{}{updateResult}}, nil
}
