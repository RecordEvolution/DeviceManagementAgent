package system

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reagent/config"
	"reagent/filesystem"
	"strings"
	"time"

	_ "embed"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

//go:embed version.txt
var version string

type System struct {
	config     *config.Config
	updateLock *semaphore.Weighted
}

type UpdateResult struct {
	CurrentVersion string
	LatestVersion  string
	DidUpdate      bool
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

func (sys *System) UpdateAgent(versionString string) error {

	// The update is already in progress
	agentDir := sys.config.CommandLineArguments.AgentDir
	remoteUpdateURL := sys.config.CommandLineArguments.RemoteUpdateURL
	agentURL := remoteUpdateURL + "/reagent-" + versionString

	newAgentDestination := fmt.Sprintf("%s/reagent-%s", agentDir, versionString)
	tmpFilePath := sys.config.CommandLineArguments.AgentDownloadDir + "/reagent-" + versionString

	if !sys.updateLock.TryAcquire(1) {
		return nil
	}

	defer sys.updateLock.Release(1)

	log.Debug().Msg("Reagent update Initialized...")

	// download it to /tmp first
	err := filesystem.DownloadURL(tmpFilePath, agentURL)
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

func (system *System) GetVersion() string {
	return version
}

func (system *System) GetLatestVersion() (string, error) {
	reagentBucketURL := system.config.CommandLineArguments.RemoteUpdateURL
	resp, err := http.Get(reagentBucketURL + "/version.txt")
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

func (system *System) UpdateIfRequired() (UpdateResult, error) {
	latestVersion, err := system.GetLatestVersion()
	if err != nil {
		return UpdateResult{}, err
	}

	currentVersion := system.GetVersion()
	log.Info().Msgf("System: Latest version: %s, Current version: %s", latestVersion, currentVersion)
	if latestVersion != currentVersion {
		log.Info().Msgf("System: Agent not up to date, downloading: v%s", latestVersion)
		err := system.UpdateAgent("v" + latestVersion)
		if err != nil {
			return UpdateResult{}, err
		}
	}

	return UpdateResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		DidUpdate:      latestVersion != currentVersion,
	}, nil
}

// ------------------------------------------------------------------------- //
