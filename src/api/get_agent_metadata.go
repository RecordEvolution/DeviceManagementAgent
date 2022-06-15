package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/release"
	"reagent/system"
)

func (ex *External) getAgentMetadataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to get agent metadata"))
	}

	currentVersion := release.GetVersion()
	OSVersion, err := system.GetOSVersion()
	if err != nil {
		return nil, err
	}

	serialNumber := ex.Config.ReswarmConfig.SerialNumber

	reswarmModeEnabled, _ := filesystem.PathExists("/opt/reagent/reswarm-mode")
	os, arch, variant := release.GetSystemInfo()
	dict := common.Dict{
		"os":           os,
		"arch":         arch,
		"variant":      variant,
		"version":      currentVersion,
		"serialNumber": serialNumber,
		"canUpdate":    reswarmModeEnabled,
	}

	latestVersion, err := ex.System.GetLatestVersion("re-agent")
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
