package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"time"
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

	prodImage := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.Version)
	tagContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	err = sm.Container.Tag(tagContext, payload.RegistryImageName.Dev, prodImage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	err = sm.HandleRegistryLoginsWithDefault(payload)
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

	reader, err := sm.Container.Push(context.Background(), prodImage, pushOptions)
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

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, false)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		return err
	}

	pushOutput, pushCmd, err := compose.Push(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(pushOutput, payload.PublishContainerName)
	if err != nil {
		return err
	}

	err = pushCmd.Wait()
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
