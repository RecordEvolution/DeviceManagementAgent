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
	OSVersion, err := system.GetOSVersion()
	if err != nil {
		return nil, err
	}

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

	if OSVersion != "" {
		dict["OSVersion"] = OSVersion
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
