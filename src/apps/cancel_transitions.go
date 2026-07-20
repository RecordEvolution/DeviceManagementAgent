package apps

import (
	"errors"
	"reagent/common"
)

func (sm *StateMachine) cancelBuild(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		return errors.New("cannot build prod apps")
	}

	buildID := common.BuildDockerBuildID(app.AppKey, app.AppName)

	if payload.DockerCompose != nil {
		compose := sm.Container.Compose()
		compose.CancelBuild(buildID) // ignore error — build may have already finished
		return sm.setState(app, common.REMOVED)
	}

	sm.Container.CancelStream(buildID)

	return sm.setState(app, common.REMOVED)
}

func (sm *StateMachine) cancelPull(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.PROD {
		return errors.New("cannot pull dev apps")
	}

	pullID := common.BuildDockerPullID(payload.AppKey, payload.AppName)

	sm.Container.CancelStream(pullID)

	return sm.setState(app, common.REMOVED)
}

func (sm *StateMachine) cancelPush(payload common.TransitionPayload, app *common.App) error {
	pushID := common.BuildDockerPushID(payload.AppKey, payload.AppName)

	sm.Container.CancelStream(pushID)

	return sm.setState(app, common.REMOVED)
}

// cancelActiveUpdate interrupts an in-flight update so its transition unwinds and
// releases the lock. Compose updates run via the docker compose CLI (not a
// docker-API stream), so CancelStream can't reach them — they are canceled
// through the update's context, which kills the CLI. The non-compose path cancels
// the docker-API pull stream. No-op if nothing is in flight.
func (sm *StateMachine) cancelActiveUpdate(payload common.TransitionPayload) {
	if payload.DockerCompose != nil {
		sm.cancelComposeUpdate(payload.Stage, payload.AppKey)
	} else {
		pullID := common.BuildDockerPullID(payload.AppKey, payload.AppName)
		sm.Container.CancelStream(pullID)
	}
}

func (sm *StateMachine) cancelUpdate(payload common.TransitionPayload, app *common.App) error {
	sm.cancelActiveUpdate(payload)

	// let the backend know the update has been canceled
	app.UpdateStatus = common.CANCELED

	return sm.setState(app, common.PRESENT)
}

func (sm *StateMachine) cancelUpdateAndRemove(payload common.TransitionPayload, app *common.App) error {
	sm.cancelActiveUpdate(payload)

	app.UpdateStatus = common.CANCELED

	return sm.removeApp(payload, app)
}

// cancelUpdateAndUninstall cancels an in-flight update and then uninstalls the
// app. It mirrors cancelUpdateAndRemove for the UPDATING -> UNINSTALLED path, so a
// teardown requested mid-update aborts the update first.
func (sm *StateMachine) cancelUpdateAndUninstall(payload common.TransitionPayload, app *common.App) error {
	sm.cancelActiveUpdate(payload)

	app.UpdateStatus = common.CANCELED

	return sm.uninstallApp(payload, app)
}

func (sm *StateMachine) cancelTransfer(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.DEV {
		return errors.New("file transfer is only for dev apps")
	}

	sm.Filesystem.CancelFileTransfer(payload.ContainerName.Dev)

	return sm.setState(app, common.REMOVED)
}
