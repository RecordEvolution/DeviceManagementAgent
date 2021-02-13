package apps

import (
	"context"
	"errors"
	"reagent/common"

	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) cancelBuild(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		return errors.New("cannot build prod apps")
	}

	buildID := common.BuildDockerBuildID(app.AppKey, app.AppName)
	ctx := context.Background()

	err := sm.Container.CancelBuild(ctx, buildID)
	if err != nil {
		log.Error().Stack().Err(err).Msg("cancel build err")
	}

	return sm.setState(app, common.REMOVED)
}
