package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) recoverFailToPresentHandler(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()
	if payload.Stage == common.DEV {
		sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Dev, map[string]interface{}{"force": true})
		return sm.buildApp(payload, app)
	}
	sm.Container.RemoveContainerByID(ctx, payload.ContainerName.Prod, map[string]interface{}{"force": true})
	return sm.pullApp(payload, app)
}
