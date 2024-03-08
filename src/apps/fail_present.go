package apps

import (
	"context"
	"reagent/common"
	"time"
)

func (sm *StateMachine) recoverFailToPresentHandler(payload common.TransitionPayload, app *common.App) error {

	var containerToRemove string
	if payload.Stage == common.DEV {
		containerToRemove = payload.ContainerName.Dev
	} else {
		containerToRemove = payload.ContainerName.Prod
	}

	removeContainerByIdContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	// remove any existing container to ensure environment variables are set
	sm.Container.RemoveContainerByID(removeContainerByIdContext, containerToRemove, map[string]interface{}{"force": true})

	if payload.Stage == common.DEV {
		return sm.buildApp(payload, app)
	}

	// Check if the image exists
	var fullImageName string
	if payload.Stage == common.DEV {
		fullImageName = payload.RegistryImageName.Dev
	} else if payload.Stage == common.PROD {
		fullImageName = payload.RegistryImageName.Prod
	}

	getImagesContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	images, err := sm.Container.GetImages(getImagesContext, fullImageName)
	if err != nil {
		return err
	}

	if len(images) != 0 {
		return sm.setState(app, common.PRESENT)
	}

	return sm.pullApp(payload, app)
}
