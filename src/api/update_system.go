package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/system"
	"strings"
	// "errors"
)

func (ex *External) getOSReleaseHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	// current release information
	osReleaseCurrent, err := system.GetOSReleaseCurrent()
	if err != nil {
		return nil, err
	}
	osReleaseVersionSplit := strings.Split(osReleaseCurrent["VERSION"], "-")
	osReleaseVersion := ""
	osReleaseBuildTime := ""
	if len(osReleaseVersionSplit) == 3 {
		osReleaseVersion = strings.Trim(osReleaseVersionSplit[0], "v")
		osReleaseBuildTime = osReleaseVersionSplit[2]
	}
	currentOSRelease := system.OSRelease{
		Name:      osReleaseCurrent["Name"],
		Version:   osReleaseVersion,
		BuildTime: osReleaseBuildTime,
	}

	// latest release information
	osReleaseLatest, err := system.GetOSReleaseLatest()
	if err != nil {
		return nil, err
	}
	newOSRelease := system.OSRelease{
		Name:      osReleaseLatest[""],
		Version:   osReleaseLatest["version"],
		BuildTime: osReleaseLatest["buildtime"],
	}

	// merge both
	osrelease := common.Dict{
		"current":   currentOSRelease,
		"available": newOSRelease,
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{osrelease},
	}, nil
}

func (ex *External) getOSUpdateHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	// prepare callback monitoring progress of download
	progressCallback := func(increment uint64, currentBytes uint64, fileSize uint64) {
		progress := common.Dict{
			"increment":    increment,
			"currentBytes": currentBytes,
			"fileSize":     fileSize,
		}

		serialNumber := ex.Config.ReswarmConfig.SerialNumber
		topic := common.BuildOSUpdateProgress(serialNumber)
		ex.LogMessenger.Publish(topics.Topic(topic), []interface{}{progress}, nil, nil)
	}

	// start downloading...
	err := system.GetOSUpdate(progressCallback)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{},
	}, nil
}

func (ex *External) installOSUpdateHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	err := system.InstallOSUpdate()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{},
	}, nil
}

func (ex *External) getInstallOSUpdateProgressHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	prog, mess, nest, err := system.GetInstallOSUpdateProgress()
	if err != nil {
		return nil, err
	}

	updateProgress := common.Dict{
		"percentage":   prog,
		"message":      mess,
		"nestingDepth": nest,
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{updateProgress},
	}, nil
}
