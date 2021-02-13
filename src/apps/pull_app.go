package apps

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
)

func (sm *StateMachine) pullApp(payload common.TransitionPayload, app *common.App) error {
	config := sm.Container.GetConfig()

	if payload.Stage == common.DEV {
		// cannot pull dev apps from registry
		return errors.New("cannot pull dev apps")
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

	fullImageNameWithVersion := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.NewestVersion)
	reader, err := sm.Container.Pull(ctx, fullImageNameWithVersion, authConfig)
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Prod, logging.PULL, reader)
	if err != nil {
		return err
	}

	if payload.NewestVersion != app.Version {
		app.Version = payload.NewestVersion
		app.ReleaseKey = payload.NewReleaseKey
		app.RequestUpdate = true
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}
