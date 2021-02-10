package apps

import (
	"github.com/rs/zerolog/log"
)

type StateSyncer struct {
	StateMachine StateMachine
	StateUpdater StateUpdater
}

func (su *StateSyncer) Sync() error {
	log.Info().Msg("Device Sync Initialized...")

	payloads, err := su.StateUpdater.GetLatestRequestedStates(true)
	if err != nil {
		return err
	}

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
