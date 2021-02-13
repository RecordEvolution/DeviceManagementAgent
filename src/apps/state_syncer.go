package apps

import (
	"github.com/rs/zerolog/log"
)

type StateSyncer struct {
	StateMachine *StateMachine
	StateUpdater *StateUpdater
}

func (su *StateSyncer) Sync() error {
	log.Info().Msg("Device Sync Initialized...")

	payloads, err := su.StateUpdater.GetLatestRequestedStates(true)
	if err != nil {
		return err
	}

	log.Info().Msgf("Checking current app states for %d apps..", len(payloads))

	for i := range payloads {

		token, err := su.StateUpdater.GetRegistryToken(payloads[i].RequestorAccountKey)
		if err != nil {
			return err
		}

		payloads[i].RegisteryToken = token
		err = su.StateMachine.RequestAppState(payloads[i])
		if err != nil {
			return err
		}
	}

	return nil
}
