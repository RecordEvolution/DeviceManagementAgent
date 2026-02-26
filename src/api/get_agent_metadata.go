package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/embedded"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/release"
)

func (ex *External) getAgentMetadataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to get agent metadata"))
	}

	currentAgentVersion := release.GetVersion()
	serialNumber := ex.Config.ReswarmConfig.SerialNumber

	reswarmModeEnabled := true //filesystem.PathExists("/opt/reagent/reswarm-mode")
	sysInfo := release.GetSystemInfo()
	dict := common.Dict{
		"os":           sysInfo.DetailedOS,
		"arch":         sysInfo.Arch,
		"variant":      sysInfo.Variant,
		"version":      currentAgentVersion,
		"serialNumber": serialNumber,
		"canUpdate":    reswarmModeEnabled,
		"OSVersion":    sysInfo.DetailedOS,
	}

	// frpc is now embedded in the binary
	frpVersion := embedded.FRP_VERSION
	frpIsLatest := true

	lastAgentVersion, err := ex.System.GetLatestVersion("re-agent")
	agentIsLatest := lastAgentVersion == currentAgentVersion
	if err == nil {
		dict["latestVersion"] = lastAgentVersion
		dict["latestTunnelVersion"] = frpVersion
		dict["latestAgentVersion"] = lastAgentVersion
		dict["hasLatest"] = agentIsLatest && frpIsLatest
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
