package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
)

func (sm *StateMachine) pullApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	config := sm.Container.GetConfig()
	if payload.Stage == common.DEV {
		err := fmt.Errorf("a dev stage app is not available on the registry")
		errorChannel <- err
		return
	}

	err := sm.setState(app, common.DOWNLOADING)
	if err != nil {
		errorChannel <- nil
		return
	}

	ctx := context.Background()

	// Need to authenticate to private registry to determine proper privileges to pull the app
	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	reader, err := sm.Container.Pull(ctx, payload.RepositoryImageName, authConfig)
	if err != nil {
		errorChannel <- err
		return
	}
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.LogManager.Stream(payload.ContainerName, logging.PULL, reader)
	if err != nil {
		errorChannel <- err
		return
	}

	errorChannel <- nil
}
