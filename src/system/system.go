package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reagent/codesign"
	"reagent/common"
	"reagent/config"
	"reagent/embedded"
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
// Reboot / Poweroff / RestartAgent live in system_unix.go and
// system_windows.go.
// ------------------------------------------------------------------------- //

// updateBaseURL returns the base URL for OTA downloads. A per-device
// update_url in the .flock config (set for appliance-managed agents) wins over
// the --remoteUpdateURL flag default, so appliances pull through the
// appliance's registry-proxy /dl routes instead of storage.googleapis.com.
func (sys *System) updateBaseURL() string {
	if sys.config.ReswarmConfig != nil && sys.config.ReswarmConfig.UpdateURL != "" {
		return sys.config.ReswarmConfig.UpdateURL
	}
	return sys.config.CommandLineArguments.RemoteUpdateURL
}

// buildBinaryDownloadURL builds the OTA URL for a published binary:
//
//	<base>/<bucket>/<goos>/<arch>/<version>/<fileName>[.exe]
//
// It is the single source of truth for the on-device URL layout, so the frpc
// sub-bucket path (bucket "re-agent/frpc") and the agent path stay in step and
// are unit-testable without a network.
func buildBinaryDownloadURL(base, bucketName, goos, arch, versionString, fileName string) string {
	url := fmt.Sprintf("%s/%s/%s/%s/%s/%s", base, bucketName, goos, arch, versionString, fileName)
	if goos == "windows" {
		url += ".exe"
	}
	return url
}

func (sys *System) downloadBinary(fileName string, bucketName string, versionString string, includeVersionString bool, progressCallback func(filesystem.DownloadProgress)) error {
	isWindows := runtime.GOOS == "windows"
	agentHomedir := sys.config.CommandLineArguments.AgentDir
	agentURL := buildBinaryDownloadURL(sys.updateBaseURL(), bucketName, runtime.GOOS, release.GetBuildArch(), versionString, fileName)

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

	// Never install a corrupted or tampered binary: verify against the
	// .sha256 manifest published next to it. Absent manifests (releases that
	// predate them) only log a warning.
	err = verifyRemoteChecksum(tmpFilePath, agentURL)
	if err != nil {
		removeErr := os.Remove(tmpFilePath)
		if removeErr != nil {
			log.Error().Err(removeErr).Msg("failed to remove download that failed checksum verification")
		}
		return err
	}

	// Authenticode: the .sha256 manifest is same-origin, so it is not an
	// authenticity anchor — the pinned signature is. Warn-only during the
	// pre-signing transition; rejected once enforcement is on (codesign.Enforcing).
	err = codesign.Verify(tmpFilePath)
	if err != nil {
		if codesign.Enforcing() {
			removeErr := os.Remove(tmpFilePath)
			if removeErr != nil {
				log.Error().Err(removeErr).Msg("failed to remove download that failed signature verification")
			}
			return fmt.Errorf("refusing to install an improperly signed binary from %s: %w", agentURL, err)
		}
		log.Warn().Err(err).Msgf("signature verification failed for %s (proceeding: pre-cutover)", agentURL)
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

// verifyRemoteChecksum compares filePath's SHA-256 against the manifest
// published at <binaryURL>.sha256 (first whitespace-separated token, hex).
// A missing manifest or a failure to FETCH it is tolerated with a warning —
// TLS to the update server remains the trust anchor and releases published
// before the manifests existed must stay installable. A manifest that exists
// and does not match is a hard error.
func verifyRemoteChecksum(filePath string, binaryURL string) error {
	manifestURL := binaryURL + ".sha256"

	body, statusCode, err := filesystem.GetRemoteFileWithStatus(manifestURL)
	if err != nil {
		log.Warn().Err(err).Msgf("could not fetch checksum manifest %s, skipping verification", manifestURL)
		return nil
	}
	defer body.Close()

	if statusCode == http.StatusNotFound {
		log.Warn().Msgf("no checksum manifest published at %s, skipping verification", manifestURL)
		return nil
	}
	if statusCode != http.StatusOK {
		log.Warn().Msgf("checksum manifest %s returned status %d, skipping verification", manifestURL, statusCode)
		return nil
	}

	manifest, err := io.ReadAll(io.LimitReader(body, 1024))
	if err != nil {
		log.Warn().Err(err).Msgf("could not read checksum manifest %s, skipping verification", manifestURL)
		return nil
	}

	expected := strings.ToLower(strings.Fields(string(manifest))[0])
	if len(expected) != sha256.Size*2 {
		log.Warn().Msgf("checksum manifest %s is malformed, skipping verification", manifestURL)
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	_, err = io.Copy(hasher, file)
	if err != nil {
		return err
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: got %s, manifest says %s", binaryURL, actual, expected)
	}

	log.Debug().Msgf("checksum verified for %s", binaryURL)
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
	fullBucketName := system.updateBaseURL() + "/" + bucketName
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
	frpcPath := filesystem.GetTunnelBinaryPath(system.config, "frpc")

	// Skip when the on-disk frpc already matches the pinned version.
	exists, err := filesystem.PathExists(frpcPath)
	if err != nil {
		return err
	}
	if exists {
		currentVersion, verErr := system.GetFrpCurrentVersion()
		if verErr != nil {
			log.Warn().Err(verErr).Msg("Could not determine frpc version, will re-acquire")
		} else if currentVersion == embedded.FRP_VERSION {
			log.Debug().Msgf("frpc v%s already present, skipping", currentVersion)
			return nil
		} else {
			log.Info().Msgf("frpc version mismatch (current: %s, want: %s), updating...", currentVersion, embedded.FRP_VERSION)
		}
	}

	// Platforms that embed frpc (Linux/macOS) extract the compiled-in blob.
	if embedded.IsEmbedded() {
		log.Info().Msgf("Extracting embedded frpc v%s to %s", embedded.FRP_VERSION, frpcPath)
		err = embedded.ExtractFrpc(frpcPath)
		if err != nil {
			return fmt.Errorf("failed to extract embedded frpc: %w", err)
		}
		return nil
	}

	// Windows: frpc is not embedded (its bytes would trip antivirus on the
	// agent binary), so it is downloaded as a separate, signed, out-of-process
	// binary. It rides the existing re-agent bucket + appliance /dl proxy via
	// the "re-agent/frpc" sub-path. The download lands at frpcPath exactly.
	log.Info().Msgf("Downloading frpc v%s to %s", embedded.FRP_VERSION, frpcPath)
	err = system.downloadBinary("frpc", "re-agent/frpc", embedded.FRP_VERSION, false, nil)
	if err != nil {
		return fmt.Errorf("failed to download frpc: %w", err)
	}

	return nil
}

func (system *System) updateFrpIfRequired(progressCallback func(filesystem.DownloadProgress)) (UpdateResult, error) {
	// frpc is pinned to embedded.FRP_VERSION; ensure the on-disk binary is
	// present and current (extracted from the embed on Linux/macOS, downloaded
	// on Windows).
	err := system.DownloadFrpIfNotExists()
	if err != nil {
		return UpdateResult{}, err
	}

	message := "frpc is embedded in binary"
	if !embedded.IsEmbedded() {
		message = "frpc is downloaded on this platform"
	}

	return UpdateResult{
		CurrentVersion: embedded.FRP_VERSION,
		LatestVersion:  embedded.FRP_VERSION,
		Message:        message,
		DidUpdate:      false,
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
	var agentUpdateResult UpdateResult

	// frpc is delivered on all supported platforms now: embedded on Linux/
	// macOS, downloaded on Windows (updateFrpIfRequired dispatches on that).
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
			agentUpdateResult = updateResult
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

	// The progress loop above can break on the last downloaded byte, before
	// the goroutines finish verifying/renaming; wait so agentUpdateResult is
	// final before acting on it.
	wg.Wait()

	if didAgentUpdate {
		// On Windows service installs this swaps the new binary into place
		// and schedules a restart; everywhere else it is a no-op (Linux
		// activation is reagent-manager.sh's job, console mode stays
		// download-only).
		system.maybeActivateAgentUpdate(agentUpdateResult)
	}

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
