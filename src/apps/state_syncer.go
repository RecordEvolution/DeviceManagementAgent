package apps

import "fmt"

type StateSyncer struct {
	StateMachine StateMachine
	StateUpdater StateUpdater
}

func (su *StateSyncer) Sync() error {
	fmt.Println("------------------------------------- STARTING DEVICE STATE SYNC -------------------------------------")

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
