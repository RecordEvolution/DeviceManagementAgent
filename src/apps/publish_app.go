package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
)

func (sm *StateMachine) publishApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.buildDevApp(payload, app, true)
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

	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: sm.Container.GetConfig().ReswarmConfig.Secret,
	}

	reader, err := sm.Container.Push(ctx, prodImage, authConfig)
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.PublishContainerName, logging.PUSH, reader)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHED)
	if err != nil {
		return err
	}

	return nil
}
