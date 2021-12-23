package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reagent/config"
	"reagent/errdefs"
	"reagent/filesystem"
	"runtime"
	"strings"
	"time"

	_ "embed"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

//go:embed version.txt
var version string
var BuildArch string = ""

type System struct {
	config     *config.Config
	updateLock *semaphore.Weighted
}

type UpdateResult struct {
	CurrentVersion string
	LatestVersion  string
	DidUpdate      bool
	InProgress     bool
	TotalFileSize  uint64
}

func New(config *config.Config) System {
	return System{
		config:     config,
		updateLock: semaphore.NewWeighted(1),
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

// ------------------------------------------------------------------------- //

func (sys *System) updateAgent(versionString string, progressCallback func(increment uint64, currentBytes uint64, totalFileSize uint64)) error {
	agentDir := sys.config.CommandLineArguments.AgentDir
	remoteUpdateURL := sys.config.CommandLineArguments.RemoteUpdateURL
	agentURL := fmt.Sprintf("%s/%s/%s/%s/reagent", remoteUpdateURL, runtime.GOOS, BuildArch, versionString)
	newAgentDestination := fmt.Sprintf("%s/reagent-v%s", agentDir, versionString)
	tmpFilePath := sys.config.CommandLineArguments.AgentDownloadDir + "/reagent-v" + versionString

	if !sys.updateLock.TryAcquire(1) {
		return errdefs.InProgress(errors.New("update already in progress"))
	}

	defer sys.updateLock.Release(1)

	log.Debug().Msgf("Attempting to download latest REagent at %s", agentURL)
	err := filesystem.DownloadURL(tmpFilePath, agentURL, progressCallback)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to download from URL: %s", agentURL)
		return err
	}

	err = os.Chmod(tmpFilePath, 0755)
	if err != nil {
		log.Error().Err(err).Msg("Failed to set permissions for agent binary")
		return err
	}

	// move it to the actual agent dir
	err = os.Rename(tmpFilePath, newAgentDestination)
	if err != nil {
		log.Error().Err(err).Msg("Failed to move agent to AgentDir")
		return err
	}

	log.Debug().Msg("Reagent update finished...")

	return nil
}

func GetVersion() string {
	return version
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

	log.Debug().Msgf("GetOSReleaseCurrent(): %+v\n", dict)

	return dict, nil
}

func GetOSReleaseLatest() (map[string]string, error) {
	osInfoBytes, err := os.ReadFile("/etc/os-release-latest.json")
	if err != nil {
		return nil, err
	}
	dict := make(map[string]string)
	err = json.Unmarshal(osInfoBytes, &dict)
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("GetOSReleaseLatest(): %+v\n", dict)

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

	log.Debug().Msgf("getOSUpdateTags(): %s : %s : %s\n", updateInfo, updateURL, updateFile)

	return updateURL, updateFile, nil
}

// ------------------------------------------------------------------------- //

// GetOSUpdate downloads the actual update-bundle to the device
func GetOSUpdate(progressCallback func(increment uint64, currentBytes uint64, totalFileSize uint64)) error {

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

func (system *System) GetLatestVersion() (string, error) {
	reagentBucketURL := system.config.CommandLineArguments.RemoteUpdateURL

	client := http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		Timeout: 10 * time.Second, // timeout for the entire request, i.e. the download itself
	}

	resp, err := client.Get(reagentBucketURL + "/version.txt")
	if err != nil {
		// happens when time is not set yet
		if strings.Contains(err.Error(), "certificate has expired or is not yet valid") {
			time.Sleep(time.Second * 1)
			return system.GetLatestVersion()
		}
		return "", err
	}
	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return "", err
	}

	return strings.Join(strings.Fields(strings.TrimSpace(buf.String())), " "), nil
}

func (system *System) Update(progressCallback func(increment uint64, currentBytes uint64, totalFileSize uint64)) (UpdateResult, error) {
	latestVersion, err := system.GetLatestVersion()
	if err != nil {
		return UpdateResult{}, err
	}

	currentVersion := GetVersion()
	log.Info().Msgf("Latest version: %s, Current version: %s", latestVersion, currentVersion)
	err = system.updateAgent(latestVersion, progressCallback)
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

func (system *System) UpdateIfRequired(progressCallback func(increment uint64, currentBytes uint64, totalFileSize uint64)) (UpdateResult, error) {
	latestVersion, err := system.GetLatestVersion()
	if err != nil {
		return UpdateResult{}, err
	}

	currentVersion := GetVersion()

	log.Info().Msgf("Should update? Latest: %s, Current: %s", latestVersion, currentVersion)
	if latestVersion == currentVersion {
		return UpdateResult{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			DidUpdate:      false,
		}, nil
	}

	log.Info().Msgf("Agent not up to date, downloading: %s", latestVersion)
	err = system.updateAgent(latestVersion, progressCallback)
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
