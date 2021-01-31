package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) stopApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	ctx := context.Background()

	err := sm.Container.StopContainerByName(ctx, payload.ContainerName, 0)

	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		errorChannel <- err
	}
}
