package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) recoverFailToPresentHandler(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		ctx := context.Background()
		sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Dev, map[string]interface{}{"force": true})
		return sm.buildApp(payload, app)
	}

	ctx := context.Background()
	sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Prod, map[string]interface{}{"force": true})
	return sm.pullApp(payload, app)
}
