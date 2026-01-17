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

func (sm *StateMachine) cancelUpdate(payload common.TransitionPayload, app *common.App) error {
	pullID := common.BuildDockerPullID(payload.AppKey, payload.AppName)

	sm.Container.CancelStream(pullID)

	// let the backend know the update has been canceled
	app.UpdateStatus = common.CANCELED

	return sm.setState(app, common.PRESENT)
}

func (sm *StateMachine) cancelUpdateAndRemove(payload common.TransitionPayload, app *common.App) error {
	pullID := common.BuildDockerPullID(payload.AppKey, payload.AppName)

	sm.Container.CancelStream(pullID)

	app.UpdateStatus = common.CANCELED

	return sm.removeApp(payload, app)
}

func (sm *StateMachine) cancelTransfer(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.DEV {
		return errors.New("file transfer is only for dev apps")
	}

	sm.Filesystem.CancelFileTransfer(payload.ContainerName.Dev)

	return sm.setState(app, common.REMOVED)
}
