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

	// The state validation will ensure it will reach it's requestedState again
	return sm.persistPostUpdateRequestedState(payload, app)
}

// persistPostUpdateRequestedState records a completed update in the local
// requested-state row: it collapses the version bookkeeping onto the freshly
// installed version so the update gate does not re-fire, and it persists the
// app's CURRENT target rather than the state this update was triggered with.
//
// The distinction is the fix for a stuck-in-PRESENT bug: an update is often
// kicked off by switching the requested state to PRESENT, and the user may
// switch it back to RUNNING while the image is still downloading. That later
// change lands in app.RequestedState (and the row) via CreateOrUpdateApp, but
// its RequestAppState is dropped because this transition holds the lock.
// Writing the trigger's requested state here would clobber manually_requested_state
// back to PRESENT, and VerifyState would then reconcile the app to PRESENT and
// never return it to RUNNING. Persisting the live target instead lets VerifyState
// drive it back to RUNNING.
func (sm *StateMachine) persistPostUpdateRequestedState(payload common.TransitionPayload, app *common.App) error {
	payload.NewestVersion = app.Version
	payload.PresentVersion = app.Version
	payload.Version = app.Version

	app.StateLock.Lock()
	payload.RequestedState = app.RequestedState
	app.StateLock.Unlock()

	return sm.StateObserver.AppStore.UpdateLocalRequestedState(payload)
}

// composeUpdateErr classifies an error from a cancelable compose command. When
// the update's context was canceled (a PRESENT request killed the CLI), the
// underlying process error is reported as a canceled stream so RequestAppState
// unwinds the transition as canceled rather than marking the app FAILED.
func composeUpdateErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errdefs.DockerStreamCanceled(err)
	}
	return err
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

	// Make the compose update cancelable. cancelUpdate (triggered by a PRESENT
	// request while UPDATING) cancels this context, which kills the running
	// `docker compose` pull/down so a hung command unwinds and releases the
	// transition lock, instead of blocking on cmd.Wait() forever. The deferred
	// cancel also frees the context on the normal (success/error) exit paths.
	ctx, cancel := context.WithCancel(context.Background())
	sm.registerComposeUpdateCancel(payload.Stage, payload.AppKey, cancel)
	defer func() {
		sm.clearComposeUpdateCancel(payload.Stage, payload.AppKey)
		cancel()
	}()

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, true)
	if err != nil {
		return err
	}

	// Tear down the whole project (services in the new file + any orphan
	// containers tagged with the same project name). Stop+Remove against the
	// new file alone would leave behind containers for services that were
	// renamed or dropped in the new compose. Volumes are preserved (no `-v`).
	_, cmd, err := compose.DownRemoveOrphansContext(ctx, dockerComposePath)
	if err != nil {
		return composeUpdateErr(ctx, err)
	}

	err = cmd.Wait()
	if err != nil {
		return composeUpdateErr(ctx, err)
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

	pullOutput, pullCmd, err := compose.PullContext(ctx, dockerComposePath)
	if err != nil {
		return composeUpdateErr(ctx, err)
	}

	_, err = sm.LogManager.StreamLogsChannel(pullOutput, payload.ContainerName.Prod)
	if err != nil {
		return composeUpdateErr(ctx, err)
	}

	err = pullCmd.Wait()
	if err != nil {
		return composeUpdateErr(ctx, err)
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

	// Promote the new compose definition to the active one, then record the
	// completed update (versions + the app's current target).
	payload.DockerCompose = payload.NewDockerCompose

	// The state validation will ensure it will reach it's requestedState again
	return sm.persistPostUpdateRequestedState(payload, app)
}
