package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) getUpdateTransition(payload common.TransitionPayload, app *common.App) TransitionFunc {
	return sm.updateApp
}

func (sm *StateMachine) updateApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.updateComposeApp(payload, app)
	}

	if payload.Stage == common.DEV {
		return errors.New("cannot update dev app")
	}

	if payload.NewestVersion == app.Version {
		return errors.New("the app is already equal to the newest version")
	}

	err := sm.setState(app, common.UPDATING)
	if err != nil {
		return err
	}

	getContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	cont, err := sm.Container.GetContainer(getContainerContext, payload.ContainerName.Prod)
	if err == nil {

		removeContainerByIdContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()
		err = sm.Container.RemoveContainerByID(removeContainerByIdContext, cont.ID, map[string]interface{}{"force": true})
		if err != nil {
			return err
		}

		pollContainerStateContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		// should return 'container not found' error, this way we know it's removed successfully
		_, errC := sm.Container.PollContainerState(pollContainerStateContext, cont.ID, time.Second)
		err := <-errC
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	}

	config := sm.Container.GetConfig()
	initMessage := fmt.Sprintf("Initialising download for the app: %s...", payload.AppName)
	err = sm.LogManager.Write(payload.ContainerName.Prod, initMessage)
	if err != nil {
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	// Need to authenticate to private registry to determine proper privileges to pull the app
	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	fullImageNameWithVersion := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.NewestVersion)
	pullOptions := container.PullOptions{
		AuthConfig: authConfig,
		PullID:     common.BuildDockerPullID(payload.AppKey, payload.AppName),
	}

	log.Debug().Msgf("PULLING IMAGE: %s", fullImageNameWithVersion)
	reader, err := sm.Container.Pull(context.Background(), fullImageNameWithVersion, pullOptions)
	if err != nil {
		errorMessage := fmt.Sprintf("Error occured while trying to pull the image: %s", err.Error())
		sm.LogManager.Write(payload.ContainerName.Prod, errorMessage)
		return err
	}

	streamErr := sm.LogManager.StreamBlocking(payload.ContainerName.Prod, common.PULL, reader)
	if streamErr != nil {
		if errdefs.IsDockerStreamCanceled(streamErr) {
			pullMessage := "The update was canceled"
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

	app.StateLock.Lock()
	app.Version = payload.NewestVersion
	app.ReleaseKey = payload.NewReleaseKey
	app.UpdateStatus = common.PENDING_REMOTE_CONFIRMATION // set flag to make backend aware we updated
	app.StateLock.Unlock()

	// also tell the database that we successfully updated (with the updated flag)
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	removeImageByNameContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	log.Debug().Msgf("Removing Old Image %s:%s", payload.RegistryImageName.Prod, payload.PresentVersion)
	sm.Container.RemoveImageByName(removeImageByNameContext, payload.RegistryImageName.Prod, payload.PresentVersion, map[string]interface{}{"force": true})

	// update the version of the local requested states
	payload.NewestVersion = app.Version
	payload.PresentVersion = app.Version
	payload.Version = app.Version

	// The state validation will ensure it will reach it's requestedState again
	return sm.StateObserver.AppStore.UpdateLocalRequestedState(payload)
}

func (sm *StateMachine) updateComposeApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return errors.New("cannot update dev app")
	}

	if payload.NewestVersion == app.Version {
		return errors.New("the app is already equal to the newest version")
	}

	err := sm.setState(app, common.UPDATING)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()
	if !compose.Supported {
		message := "Docker Compose is not supported for this device"
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, message)
		if writeErr != nil {
			return writeErr
		}

		return errdefs.DockerComposeNotSupported(errors.New("docker compose is not supported"))
	}

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, true)
	if err != nil {
		return err
	}

	_, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, cmd, err = compose.Remove(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	initMessage := fmt.Sprintf("Initialising download for the app: %s...", payload.AppName)
	err = sm.LogManager.Write(payload.ContainerName.Prod, initMessage)
	if err != nil {
		return err
	}

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.ContainerName.Prod, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	pullOutput, pullCmd, err := compose.Pull(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(pullOutput, payload.ContainerName.Prod)
	if err != nil {
		return err
	}

	err = pullCmd.Wait()
	if err != nil {
		return err
	}

	pullMessage := fmt.Sprintf("Succesfully installed the app: %s (Version: %s)", payload.AppName, payload.NewestVersion)
	writeErr := sm.LogManager.Write(payload.ContainerName.Prod, pullMessage)
	if writeErr != nil {
		return writeErr
	}

	app.StateLock.Lock()
	app.Version = payload.NewestVersion
	app.ReleaseKey = payload.NewReleaseKey
	app.UpdateStatus = common.PENDING_REMOTE_CONFIRMATION // set flag to make backend aware we updated
	app.StateLock.Unlock()

	// also tell the database that we successfully updated (with the updated flag)
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	// TODO: remove old images from docker-compose

	// update the version of the local requested states
	payload.DockerCompose = payload.NewDockerCompose
	payload.NewestVersion = app.Version
	payload.PresentVersion = app.Version
	payload.Version = app.Version

	// The state validation will ensure it will reach it's requestedState again
	return sm.StateObserver.AppStore.UpdateLocalRequestedState(payload)
}
