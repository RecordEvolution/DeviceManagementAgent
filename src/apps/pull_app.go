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
		return nil
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

	// TODO: properly handle multiple versions
	var version string
	if payload.NewestVersion != payload.PresentVersion {
		version = payload.NewestVersion
	} else if payload.NewestVersion != payload.Version {
		version = payload.NewestVersion
	}

	fmt.Println("Versions:", "PV:", payload.PresentVersion, "NV:", payload.NewestVersion, "V:", payload.Version)

	if version == "" {
		return errors.New("version string missing from payload")
	}

	fullImageNameWithVersion := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, version)
	reader, err := sm.Container.Pull(ctx, fullImageNameWithVersion, authConfig)
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
