package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) recoverFailToPresentHandler(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	var containerToRemove string
	if payload.Stage == common.DEV {
		containerToRemove = payload.ContainerName.Dev
	} else {
		containerToRemove = payload.ContainerName.Prod
	}

	// remove any existing container to ensure environment variables are set
	sm.Container.RemoveContainerByID(ctx, containerToRemove, map[string]interface{}{"force": true})

	if payload.Stage == common.DEV {
		return sm.buildApp(payload, app)
	}

	return sm.pullApp(payload, app)
}
