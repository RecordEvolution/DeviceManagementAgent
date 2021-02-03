package apps

import (
	"context"
	"reagent/common"
)

func (sm *StateMachine) removeApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.PROD {
		ctx := context.Background()
		err := sm.Container.RemoveImageByName(ctx, payload.RegistryImageName.Prod, payload.Version, nil)
		// if container exists remove container first and then remove image
		if err != nil {
			return err
		}
	}

	return nil
}
