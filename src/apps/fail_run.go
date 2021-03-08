package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) recoverFailToRunningHandler(payload common.TransitionPayload, app *common.App) error {
	// make sure to remove any existing container to ensure environment variables are set
	ctx := context.Background()
	if payload.Stage == common.DEV {
		sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Dev, map[string]interface{}{"force": true})
	} else {
		sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Prod, map[string]interface{}{"force": true})
	}

	return sm.runApp(payload, app)
}
