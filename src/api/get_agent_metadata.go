package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"reagent/system"
	"runtime"
)

func (ex *External) getAgentMetadataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	currentVersion := system.GetVersion()
	serialNumber := ex.Config.ReswarmConfig.SerialNumber

	dict := common.Dict{
		"oos":          runtime.GOOS,
		"arch":         runtime.GOARCH,
		"version":      currentVersion,
		"serialNumber": serialNumber,
	}

	latestVersion, err := ex.System.GetLatestVersion()
	if err == nil {
		dict["latestVersion"] = latestVersion
		dict["hasLatest"] = latestVersion == currentVersion
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
