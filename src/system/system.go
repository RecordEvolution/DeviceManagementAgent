package system

import (
	"fmt"
	"os"
	"os/exec"
	"reagent/config"
	"reagent/filesystem"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type System struct {
	config     *config.Config
	updateLock *semaphore.Weighted
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

func (sys *System) UpdateAgent(versionString string) chan error {

	// The update is already in progress
	agentDir := sys.config.CommandLineArguments.AgentDir
	remoteUpdateURL := sys.config.CommandLineArguments.RemoteUpdateURL
	agentURL := remoteUpdateURL + "/reagent-" + versionString

	newAgentDestination := fmt.Sprintf("%s/reagent-%s", agentDir, versionString)
	tmpFilePath := sys.config.CommandLineArguments.AgentDownloadDir + "/reagent-" + versionString
	errC := make(chan error, 1)

	go func() {
		if !sys.updateLock.TryAcquire(1) {
			return
		}

		defer sys.updateLock.Release(1)

		log.Debug().Msg("Reagent update Initialized...")

		// download it to /tmp first
		err := filesystem.DownloadURL(tmpFilePath, agentURL)
		if err != nil {
			errC <- err
			log.Error().Err(err).Msgf("Failed to download from URL: %s", agentURL)
			return
		}

		// move it to the actual agent dir
		err = os.Rename(tmpFilePath, newAgentDestination)
		if err != nil {
			errC <- err
			log.Error().Err(err).Msg("Failed to move agent to AgentDir")
			return
		}

		err = os.Chmod(newAgentDestination, 0755)
		if err != nil {
			errC <- err
			log.Error().Err(err).Msg("Failed to set permissions for agent binary")
			return
		}

		log.Debug().Msg("Reagent update finished...")

		errC <- nil
	}()

	return errC
}

// ------------------------------------------------------------------------- //
