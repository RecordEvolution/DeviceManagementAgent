package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) recoverFailToRunningHandler(payload common.TransitionPayload, app *common.App) error {

	var containerToRemove string
	if payload.Stage == common.DEV {
		containerToRemove = payload.ContainerName.Dev
	} else {
		containerToRemove = payload.ContainerName.Prod
	}

	if payload.DockerCompose == nil {
		ctx := context.Background()
		// remove any existing container to ensure environment variables are set
		sm.Container.RemoveContainerByID(ctx, containerToRemove, map[string]interface{}{"force": true})
	}

	return sm.runApp(payload, app)
}
