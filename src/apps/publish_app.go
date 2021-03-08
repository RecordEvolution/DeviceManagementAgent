package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
)

func (sm *StateMachine) publishApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("Initializing publish process for %s...", app.AppName))
	if err != nil {
		return err
	}

	err = sm.buildDevApp(payload, app, true)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.PublishContainerName, "App build has finished, Starting to publish...")
	if err != nil {
		return err
	}

	ctx := context.Background()
	prodImage := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.Version)
	err = sm.Container.Tag(ctx, payload.RegistryImageName.Dev, prodImage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	pushOptions := container.PushOptions{
		AuthConfig: container.AuthConfig{
			Username: payload.RegisteryToken,
			Password: sm.Container.GetConfig().ReswarmConfig.Secret,
		},
		PushID: common.BuildDockerPushID(payload.AppKey, payload.AppName),
	}

	reader, err := sm.Container.Push(ctx, prodImage, pushOptions)
	if err != nil {
		return err
	}

	err = sm.LogManager.StreamBlocking(payload.PublishContainerName, common.PUSH, reader)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHED)
	if err != nil {
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.PublishContainerName)
	if err != nil {
		return err
	}

	return nil
}
