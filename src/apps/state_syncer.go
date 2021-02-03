package apps

type StateSyncer struct {
	StateMachine StateMachine
	StateUpdater StateUpdater
}

func (su *StateSyncer) Sync() error {
	payloads, err := su.StateUpdater.GetLatestRequestedStates(true)
	if err != nil {
		return err
	}

	for i := range payloads {
		payload := payloads[i]

		token, err := su.StateUpdater.GetRegistryToken(payload.RequestorAccountKey)
		if err != nil {
			return err
		}

		payloads[i].RegisteryToken = token

		err = su.StateMachine.RequestAppState(payload)
		if err != nil {
			return err
		}
	}

	return nil
}
