package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/release"
	"reagent/safe"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/Masterminds/semver"
	"github.com/rs/zerolog/log"
)

type System struct {
	config    *config.Config
	messenger messenger.Messenger
}

type UpdateResult struct {
	CurrentVersion  string
	LatestVersion   string
	Message         string
	DidAgentUpdate  bool
	DidTunnelUpdate bool
	DidUpdate       bool
	InProgress      bool
	TotalFileSize   uint64
}

func New(config *config.Config, messenger messenger.Messenger) System {
	return System{
		config:    config,
		messenger: messenger,
	}
}

// ------------------------------------------------------------------------- //

func (sys *System) Reboot() error {
	_, err := exec.Command("reboot").Output()
	return err
}

func (sys *System) Poweroff() error {
	_, err := exec.Command("poweroff").Output()
	return err
}

func (sys *System) RestartAgent() error {
	_, err := exec.Command("systemctl", "restart", "reagent").Output()
	return err
}

// ------------------------------------------------------------------------- //

func (sys *System) downloadBinary(fileName string, bucketName string, versionString string, includeVersionString bool, progressCallback func(filesystem.DownloadProgress)) error {
	isWindows := runtime.GOOS == "windows"
	agentHomedir := sys.config.CommandLineArguments.AgentDir
	remoteUpdateURL := sys.config.CommandLineArguments.RemoteUpdateURL + "/" + bucketName
	agentURL := fmt.Sprintf("%s/%s/%s/%s/%s", remoteUpdateURL, runtime.GOOS, release.GetBuildArch(), versionString, fileName)
	if isWindows {
		agentURL += ".exe"
	}

	var actualFileDestination string
	if includeVersionString {
		actualFileDestination = fmt.Sprintf("%s/%s-v%s", agentHomedir, fileName, versionString)
	} else {
		actualFileDestination = fmt.Sprintf("%s/%s", agentHomedir, fileName)
	}

	if isWindows {
		actualFileDestination += ".exe"
	}

	tmpFilePath := sys.config.CommandLineArguments.DownloadDir + "/" + fileName + "-v" + versionString
	log.Debug().Msgf("Attempting to download latest %s binary at %s", fileName, agentURL)
	err := filesystem.DownloadURL(tmpFilePath, agentURL, progressCallback)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to download from URL: %s", agentURL)
		return err
	}

	err = os.Chmod(tmpFilePath, 0755)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to set permissions for %s binary", fileName)
		return err
	}

	err = os.Rename(tmpFilePath, actualFileDestination)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to move %s binary to AgentDir", fileName)
		return err
	}

	return nil
}

func GetOSReleaseCurrent() (map[string]string, error) {
	osInfoBytes, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil, err
	}
	osInfoString := string(osInfoBytes)
	osInfoSplit := strings.Split(osInfoString, "\n")

	dict := make(map[string]string)
	for _, line := range osInfoSplit {
		keyValuePair := strings.Split(line, "=")
		if len(keyValuePair) == 2 {
			key := keyValuePair[0]
			var value string

			valueSplit := strings.Split(keyValuePair[1], "\"")
			if len(valueSplit) == 3 {
				value = valueSplit[1]
			} else {
				value = valueSplit[0]
			}

			dict[key] = value
		}
	}

	return dict, nil
}

func GetOSReleaseLatest() (map[string]interface{}, error) {
	osInfoBytes, err := os.ReadFile("/etc/os-release-latest.json")
	if err != nil {
		return nil, err
	}
	dict := make(map[string]interface{})
	err = json.Unmarshal(osInfoBytes, &dict)
	if err != nil {
		return nil, err
	}

	return dict, nil
}

func GetOSVersion() (string, error) {
	switch runtime.GOOS {
	case "linux":
		osRelease, err := GetOSReleaseCurrent()
		if err == nil {
			prettyName := osRelease["PRETTY_NAME"]
			if prettyName != "" {
				return prettyName, nil
			}

			name := osRelease["NAME"]
			if name != "" {
				return name, nil
			}
		}

		return "Linux/Unix-based", nil
	case "windows":
		return "Windows", nil
	case "darwin":
		return "MacOS", nil
	}

	return "", nil
}

// getOSUpdateTags read and extracts info tags about latest available update
func getOSUpdateTags() (string, string, error) {

	// obtain release URL from OS that observes and logs latest OS version
	updateInfoBytes, err := os.ReadFile("/etc/reswarmos-update")
	if err != nil {
		return "", "", err
	}
	updateInfo := strings.Split(string(updateInfoBytes), "\n")[0]
	updateURL := strings.Split(updateInfo, ",")[2]
	updateURLSplit := strings.Split(updateURL, "/")
	updateFile := updateURLSplit[len(updateURLSplit)-1]

	log.Debug().Msgf("getOSUpdateTags(): %s : %s : %s", updateInfo, updateURL, updateFile)

	return updateURL, updateFile, nil
}

// ------------------------------------------------------------------------- //

// GetOSUpdate downloads the actual update-bundle to the device
func GetOSUpdate(progressCallback func(filesystem.DownloadProgress)) error {

	// find release tags from update file regularly updated by the system
	updateURL, updateFile, err := getOSUpdateTags()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to obtain update tags")
	}

	log.Debug().Msg("Starting to download ReswarmOS bundle from " + updateURL + " to /tmp/" + updateFile)

	// download update bundle at given URL
	err = filesystem.DownloadURL("/tmp/"+updateFile, updateURL, progressCallback)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to download from URL: %s", updateURL)
		return err
	}

	log.Debug().Msg("ReswarmOS update bundle download finished from " + updateURL + "...")

	return nil
}

// InstallOSUpdate installs the latest update-bundle available on the device
func InstallOSUpdate(progressCallback func(operationName string, progressPercent uint64)) error {

	// find update-bundle installer file
	_, updateFile, err := getOSUpdateTags()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to obtain update tags")
	}
	bundleFile := "/tmp/" + updateFile

	log.Debug().Msg("installing ReswarmOS update bundle " + bundleFile)

	err = raucInstallBundle(bundleFile, progressCallback)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to install ReswarmOS update bundle")
	}

	return nil
}

func GetEnvironment(config *config.Config) string {
	env := config.ReswarmConfig.Environment
	if env != "" {
		return env
	}

	endpoint := config.ReswarmConfig.DeviceEndpointURL
	if strings.Contains(endpoint, "datapods") {
		return "test"
	} else if strings.Contains(endpoint, "record-evolution") {
		return "production"
	}

	return "local"
}

func (system *System) GetLatestVersion(bucketName string) (string, error) {
	fullBucketName := system.config.CommandLineArguments.RemoteUpdateURL + "/" + bucketName
	resp, err := filesystem.GetRemoteFile(fullBucketName + "/availableVersions.json")
	if err != nil {
		return "", err
	}

	var environmentVersionMap map[string]string
	json.NewDecoder(resp).Decode(&environmentVersionMap)

	versionString := environmentVersionMap[GetEnvironment(system.config)]
	if versionString == "" {
		versionString = environmentVersionMap["all"]
	}

	return versionString, nil
}

func (system *System) compareVersion(currentVersion string, latestVersion string) (bool, []error, error) {
	constraint, err := semver.NewConstraint(fmt.Sprintf("> %s", currentVersion))
	if err != nil {
		return false, nil, err
	}

	newVersion, err := semver.NewVersion(latestVersion)
	if err != nil {
		return false, nil, err
	}

	shouldUpdate, errors := constraint.Validate(newVersion)
	return shouldUpdate, errors, nil
}

func (system *System) DownloadFrpIfNotExists() error {
	latestVersion, err := system.GetLatestVersion("frpc")
	if err != nil {
		return err
	}

	exists := true

	_, err = system.GetFrpCurrentVersion()
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			exists = false
		} else {
			return err
		}
	}

	if exists {
		return nil
	}

	err = system.downloadBinary("frpc", "frpc", latestVersion, false, nil)
	if err != nil {
		return err
	}

	return nil
}

func (system *System) updateFrpIfRequired(progressCallback func(filesystem.DownloadProgress)) (UpdateResult, error) {
	latestVersion, err := system.GetLatestVersion("frpc")
	if err != nil {
		return UpdateResult{}, err
	}

	exists := true
	var shouldUpdate bool
	var errorsArr []error

	currentVersion, err := system.GetFrpCurrentVersion()
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			exists = false
			shouldUpdate = true
		} else {
			return UpdateResult{}, err
		}
	}

	if exists {
		shouldUpdate, errorsArr, err = system.compareVersion(currentVersion, latestVersion)
		if err != nil {
			return UpdateResult{}, err
		}
	}

	if !shouldUpdate {
		return UpdateResult{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Message:        fmt.Sprintf("%+v", errorsArr),
			DidUpdate:      false,
		}, nil
	}

	err = system.downloadBinary("frpc", "frpc", latestVersion, false, progressCallback)
	if err != nil {
		if errdefs.IsInProgress(err) {
			return UpdateResult{
				CurrentVersion: currentVersion,
				LatestVersion:  latestVersion,
				DidUpdate:      false,
				InProgress:     true,
			}, err
		}
		return UpdateResult{}, err
	}

	return UpdateResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		DidUpdate:      true,
	}, nil
}

func (system *System) GetFrpCurrentVersion() (string, error) {
	frpPath := filesystem.GetTunnelBinaryPath(system.config, "frpc")

	exists, err := filesystem.PathExists(frpPath)
	if err != nil {
		return "", err
	}

	if !exists {
		return "", errdefs.ErrNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	cmd := exec.CommandContext(ctx, frpPath, "--version")
	stdout, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(stdout)), nil
}

func (system *System) UpdateDeviceMetadata() error {
	ctx := context.Background()
	args := []interface{}{common.Dict{"device_key": system.config.ReswarmConfig.DeviceKey}}
	res, err := system.messenger.Call(ctx, topics.GetDeviceMetadata, args, nil, nil, nil)
	if err != nil {
		return err
	}

	resultPayload, ok := res.Arguments[0].(map[string]interface{})
	if !ok {
		return errors.New("invalid payload")
	}

	deviceName := fmt.Sprint(resultPayload["device_name"])
	swarmName := fmt.Sprint(resultPayload["swarm_name"])
	swarmOwnerName := fmt.Sprint(resultPayload["ownername"])
	swarmKey, ok := resultPayload["swarm_key"].(uint64)
	if !ok {
		return errors.New("swarm_key has invalid type")
	}

	system.config.ReswarmConfig.Name = deviceName
	system.config.ReswarmConfig.SwarmName = swarmName
	system.config.ReswarmConfig.SwarmOwnerName = swarmOwnerName
	system.config.ReswarmConfig.SwarmKey = int(swarmKey)

	err = config.SaveReswarmConfig(system.config.CommandLineArguments.ConfigFileLocation, system.config.ReswarmConfig)

	return err
}

func (system *System) UpdateSystem(progressCallback func(filesystem.DownloadProgress), updateAgent bool) (UpdateResult, error) {
	var wg sync.WaitGroup

	progressChan := make(chan filesystem.DownloadProgress)
	progressFunction := func(dp filesystem.DownloadProgress) {
		progressChan <- dp
	}

	startUpdate := time.Now()

	didTunnelUpdate := false
	didAgentUpdate := false

	if runtime.GOOS != "windows" {
		wg.Add(1)
		safe.Go(func() {

			defer wg.Done()

			updateResult, err := system.updateFrpIfRequired(progressFunction)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("Failed to update frpc.. continuing...")
			}

			if !updateResult.DidUpdate {
				log.Debug().Msgf("Not downloading frpc because: %s", updateResult.Message)
			}

			didTunnelUpdate = updateResult.DidUpdate
		})
	}

	if updateAgent {
		wg.Add(1)
		safe.Go(func() {

			defer wg.Done()

			updateResult, err := system.updateAgentIfRequired(progressFunction)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("Failed to update.. continuing...")
			}

			if !updateResult.DidUpdate {
				log.Debug().Msgf("Not downloading Agent because: %s", updateResult.Message)
			}

			didAgentUpdate = updateResult.DidUpdate
		})
	}

	safe.Go(func() {
		wg.Wait()

		// if all download processes end, make sure to close the channel so we don't wait
		close(progressChan)
	})

	bufferProgress := &filesystem.DownloadProgress{
		Increment:     0,
		CurrentBytes:  0,
		TotalFileSize: 0,
	}

	progressMap := make(map[string]*filesystem.DownloadProgress)
	for progress := range progressChan {
		currentProgress := &filesystem.DownloadProgress{}
		if progressMap[progress.FilePath] == nil {
			progressMap[progress.FilePath] = &filesystem.DownloadProgress{
				Increment:     progress.Increment,
				CurrentBytes:  progress.CurrentBytes,
				FilePath:      progress.FilePath,
				TotalFileSize: progress.TotalFileSize,
			}
		} else {
			currentProgress = progressMap[progress.FilePath]
		}

		currentProgress.Increment = progress.Increment
		currentProgress.CurrentBytes = progress.CurrentBytes

		for _, storedProgress := range progressMap {
			bufferProgress.Increment = storedProgress.Increment
			bufferProgress.CurrentBytes += storedProgress.CurrentBytes
			bufferProgress.TotalFileSize += storedProgress.TotalFileSize
		}

		if progressCallback != nil {
			progressCallback(*bufferProgress)
		}

		if bufferProgress.CurrentBytes == bufferProgress.TotalFileSize {
			break
		}

		bufferProgress.Increment = 0
		bufferProgress.CurrentBytes = 0
		bufferProgress.TotalFileSize = 0
	}

	updateTime := time.Since(startUpdate)

	log.Debug().Msgf("Time it took to update system: %s", updateTime)

	return UpdateResult{
		DidAgentUpdate:  didAgentUpdate,
		DidTunnelUpdate: didTunnelUpdate,
		DidUpdate:       didTunnelUpdate || didAgentUpdate,
		InProgress:      false,
	}, nil
}

func (system *System) updateAgentIfRequired(progressCallback func(filesystem.DownloadProgress)) (UpdateResult, error) {
	latestVersion, err := system.GetLatestVersion("re-agent")
	if err != nil {
		return UpdateResult{}, err
	}

	currentVersion := release.GetVersion()

	log.Info().Msgf("Should update? Latest: %s, Current: %s", latestVersion, currentVersion)

	shouldUpdate, errorsArr, err := system.compareVersion(currentVersion, latestVersion)
	if err != nil {
		return UpdateResult{}, err
	}

	if !shouldUpdate {
		return UpdateResult{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Message:        fmt.Sprintf("%+v", errorsArr),
			DidUpdate:      false,
		}, nil
	}

	log.Info().Msgf("Agent not up to date, downloading: %s", latestVersion)
	err = system.downloadBinary("reagent", "re-agent", latestVersion, true, progressCallback)
	if err != nil {
		if errdefs.IsInProgress(err) {
			return UpdateResult{
				CurrentVersion: currentVersion,
				LatestVersion:  latestVersion,
				DidUpdate:      false,
				InProgress:     true,
			}, err
		}
		return UpdateResult{}, err
	}

	return UpdateResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		DidUpdate:      true,
	}, nil
}

// ------------------------------------------------------------------------- //
