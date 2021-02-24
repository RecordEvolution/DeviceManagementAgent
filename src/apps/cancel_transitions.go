package apps

import (
	"context"
	"errors"
	"reagent/common"
)

func (sm *StateMachine) cancelBuild(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		return errors.New("cannot build prod apps")
	}

	buildID := common.BuildDockerBuildID(app.AppKey, app.AppName)
	ctx := context.Background()

	// ignore potential error
	_ = sm.Container.CancelStream(ctx, buildID)

	return sm.setState(app, common.REMOVED)
}

func (sm *StateMachine) cancelPull(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.PROD {
		return errors.New("cannot pull dev apps")
	}

	pullID := common.BuildDockerPullID(payload.AppKey, payload.AppName)
	ctx := context.Background()

	// ignore potential error
	_ = sm.Container.CancelStream(ctx, pullID)

	return sm.setState(app, common.REMOVED)
}
