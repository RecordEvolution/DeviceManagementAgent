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

	fmt.Println("------------------------------------- COMPLETED DEVICE STATE SYNC -------------------------------------")

	return nil
}
