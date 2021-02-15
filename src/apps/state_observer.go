package apps

import (
	"context"
	"reagent/common"
	"reagent/container"

	"github.com/rs/zerolog/log"
)

type StateObserver struct {
	Container    container.Container
	StateUpdater *StateUpdater
}

// Notify verifies a changed state in the StateMachine and stores it in the database
func (so *StateObserver) Notify(app *common.App, achievedState common.AppState) error {
	// doublecheck if state is actually achievable and set the state in the database
	_, err := so.StateUpdater.UpdateAppState(app, achievedState)
	if err != nil {
		return err
	}
	return nil
}

func (so *StateObserver) ObserveAppState() chan error {
	observerErrChan := make(chan error, 1)

	eventC, errC := so.Container.ObserveAllContainerStatus(context.Background())

	go func() {
		log.Debug().Msg("Started observering app states....")
		for {
			select {
			case event := <-eventC:
				log.Debug().Msgf("%+v", event)
			case err := <-errC:
				observerErrChan <- err
				log.Error().Err(err).Msg("obbserver error")
				return
			}
		}
	}()

	return observerErrChan
}
