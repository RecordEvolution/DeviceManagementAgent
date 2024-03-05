package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
)

func (sm *StateMachine) publishApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.publishComposeApp(payload, app)
	}

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

	streamErr := sm.LogManager.StreamBlocking(payload.PublishContainerName, common.PUSH, reader)
	if streamErr != nil {
		if errdefs.IsDockerStreamCanceled(streamErr) {
			pushMessage := "The app release was canceled"
			writeErr := sm.LogManager.Write(payload.PublishContainerName, pushMessage)
			if writeErr != nil {
				return writeErr
			}
			// this error will not cause a failed state and is handled upstream
			return streamErr
		}

		return streamErr
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

func (sm *StateMachine) publishComposeApp(payload common.TransitionPayload, app *common.App) error {
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

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	config := sm.Container.GetConfig()

	loginStdout, loginStderr, _, err := compose.Login(config.ReswarmConfig.DockerRegistryURL, payload.RegisteryToken, config.ReswarmConfig.Secret)
	if err != nil {
		return err
	}

	err = sm.LogManager.StreamChannel(payload.PublishContainerName, common.PULL, loginStdout)
	if err != nil {
		return err
	}

	err = <-loginStderr
	if err != nil {
		sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("The app failed to login, reason: %s\n", err.Error()))
		return err
	}

	pushStdout, pushStderr, _, err := compose.Push(dockerComposePath)
	if err != nil {
		return err
	}

	err = sm.LogManager.StreamChannel(payload.PublishContainerName, common.PUSH, pushStdout)
	if err != nil {
		return err
	}

	err = <-pushStderr
	if err != nil {
		sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("The app failed to push, reason: %s\n", err.Error()))
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
