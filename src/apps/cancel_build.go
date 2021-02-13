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
	_ = sm.Container.CancelBuild(ctx, buildID)

	return sm.setState(app, common.REMOVED)
}
