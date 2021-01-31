package apps

import (
	"context"
	"fmt"
	"reagent/common"
)

func (sm *StateMachine) stopBuild(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	id := sm.LogManager.GetActiveBuildId(payload.ContainerName)
	if id != "" {
		ctx := context.Background()
		err := sm.Container.CancelBuild(ctx, id)
		if err != nil {
			errorChannel <- err
			return
		}
	}

	fmt.Println("No active build was found.")
}
