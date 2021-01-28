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

	for _, payload := range payloads {
		err = su.StateMachine.RequestAppState(payload)
		if err != nil {
			return err
		}
	}

	return nil
}
