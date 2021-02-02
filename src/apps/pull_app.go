package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
)

func (sm *StateMachine) pullApp(payload common.TransitionPayload, app *common.App) error {
	config := sm.Container.GetConfig()
	if payload.Stage == common.DEV {
		err := fmt.Errorf("a dev stage app is not available on the registry")
		return err
	}

	err := sm.setState(app, common.DOWNLOADING)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Need to authenticate to private registry to determine proper privileges to pull the app
	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	reader, err := sm.Container.Pull(ctx, payload.RepositoryImageName.Prod, authConfig)
	if err != nil {
		return err
	}
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Prod, logging.PULL, reader)
	if err != nil {
		return err
	}

	return nil
}
