package apps

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
)

func (sm *StateMachine) pullApp(payload common.TransitionPayload, app *common.App) error {
	config := sm.Container.GetConfig()

	if payload.Stage == common.DEV {
		// cannot pull dev apps from registry
		return errors.New("cannot pull dev apps")
	}

	err := sm.LogManager.ClearRemote(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	initMessage := fmt.Sprintf("Initialising download for the app: %s...", payload.AppName)
	err = sm.LogManager.Write(payload.ContainerName.Prod, initMessage)
	if err != nil {
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Need to authenticate to private registry to determine proper privileges to pull the app
	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	fullImageNameWithVersion := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.NewestVersion)
	pullOptions := container.PullOptions{
		AuthConfig: authConfig,
		PullID:     common.BuildDockerPullID(payload.AppKey, payload.AppName),
	}

	reader, err := sm.Container.Pull(ctx, fullImageNameWithVersion, pullOptions)
	if err != nil {
		errorMessage := fmt.Sprintf("Error occured while trying to pull the image: %s", err.Error())
		sm.LogManager.Write(payload.ContainerName.Prod, errorMessage)
		return err
	}

	err = sm.setState(app, common.DOWNLOADING)
	if err != nil {
		return err
	}

	streamErr := sm.LogManager.StreamBlocking(payload.ContainerName.Prod, common.PULL, reader)
	if streamErr != nil {
		if errdefs.IsDockerStreamCanceled(streamErr) {
			pullMessage := "The app download was canceled"
			writeErr := sm.LogManager.Write(payload.ContainerName.Prod, pullMessage)
			if writeErr != nil {
				return writeErr
			}
			// this error will not cause a failed state and is handled upstream
			return streamErr
		}

		return streamErr
	}

	pullMessage := fmt.Sprintf("Succesfully installed the app: %s (Version: %s)", payload.AppName, payload.NewestVersion)
	writeErr := sm.LogManager.Write(payload.ContainerName.Prod, pullMessage)
	if writeErr != nil {
		return writeErr
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}
