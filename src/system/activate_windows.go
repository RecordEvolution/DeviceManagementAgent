//go:build windows

package system

import (
	"reagent/lifecycle"
	"reagent/safe"
	"reagent/selfupdate"
	"time"

	"github.com/rs/zerolog/log"
)

// maybeActivateAgentUpdate swaps a freshly downloaded agent version into the
// service binary path and requests a supervised restart — the in-process
// equivalent of what reagent-manager.sh does externally on Linux. Only active
// in service mode: console runs (development, FlockFlasher test devices) keep
// the historical download-only behavior.
func (system *System) maybeActivateAgentUpdate(result UpdateResult) {
	if !result.DidAgentUpdate && !result.DidUpdate {
		return
	}

	if !lifecycle.Supervised() {
		log.Info().Msgf("agent v%s downloaded but not activated: running in console mode (install the Windows service for automatic updates)", result.LatestVersion)
		return
	}

	manager := selfupdate.New(system.config.CommandLineArguments.AgentDir)
	if manager.IsVersionBlocked(result.LatestVersion) {
		log.Warn().Msgf("agent v%s previously failed on this device, not activating it again", result.LatestVersion)
		return
	}

	safe.Go(func() {
		// Give the update_agent WAMP response and the progress publishes time
		// to flush before the process restarts (the Linux manager has a
		// comparable <=15s activation lag).
		time.Sleep(10 * time.Second)

		err := manager.Activate(result.LatestVersion, result.CurrentVersion)
		if err != nil {
			log.Error().Err(err).Msgf("failed to activate agent update v%s", result.LatestVersion)
			return
		}

		err = lifecycle.RequestRestart("agent update v" + result.LatestVersion)
		if err != nil {
			log.Error().Err(err).Msg("activated the agent update but failed to request a restart")
		}
	})
}
