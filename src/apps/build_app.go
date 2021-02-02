package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"

	"github.com/docker/docker/api/types"
)

func (sm *StateMachine) buildApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		err := sm.buildDevApp(payload, app)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateMachine) buildDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background() // TODO: store context in memory for build cancellation

	config := sm.Container.GetConfig()
	fileDir := config.CommandLineArguments.AppBuildsDirectory
	fileName := payload.AppName + config.CommandLineArguments.CompressedBuildExtension
	filePath := fileDir + "/" + fileName

	exists, _ := filesystem.FileExists(filePath)
	if !exists {
		sm.setState(app, common.FAILED)
		err := fmt.Errorf("build files do not exist on path %s", filePath)
		return err
	}

	err := sm.setState(app, common.BUILDING)

	if err != nil {
		return err
	}

	reader, err := sm.Container.Build(ctx, filePath, types.ImageBuildOptions{Tags: []string{payload.RepositoryImageName.Dev}, Dockerfile: "Dockerfile"})
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.ContainerName.Dev, logging.BUILD, reader)

	buildFailed := false
	if err != nil {
		if errdefs.IsBuildFailed(err) {
			buildFailed = true
		} else {
			return err
		}
	}

	// need to specify that this is a release build on remote update
	// this ensures that the dev release will be set to exists = true
	// prod ready builds will not be set to exists until after they are pushed
	app.ReleaseBuild = false
	app.ManuallyRequestedState = common.PRESENT
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	buildResultMessage := "Image built successfully"
	if buildFailed {
		buildResultMessage = "Image build failed to complete"
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("%s", buildResultMessage))
	if err != nil {
		return err
	}

	return nil
}
