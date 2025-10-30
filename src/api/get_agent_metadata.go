package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/embedded"
	"reagent/errdefs"
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

	currentAgentVersion := release.GetVersion()
	OSVersion, err := system.GetOSVersion()
	if err != nil {
		return nil, err
	}

	serialNumber := ex.Config.ReswarmConfig.SerialNumber

	reswarmModeEnabled := true //filesystem.PathExists("/opt/reagent/reswarm-mode")
	os, arch, variant := release.GetSystemInfo()
	dict := common.Dict{
		"os":           os,
		"arch":         arch,
		"variant":      variant,
		"version":      currentAgentVersion,
		"serialNumber": serialNumber,
		"canUpdate":    reswarmModeEnabled,
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

	if OSVersion != "" {
		dict["OSVersion"] = OSVersion
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
