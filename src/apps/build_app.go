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
		err := sm.buildDevApp(payload, app, false)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateMachine) buildDevApp(payload common.TransitionPayload, app *common.App, releaseBuild bool) error {
	ctx := context.Background() // TODO: store context in memory for build cancellation

	sm.setState(app, common.REMOVED)

	config := sm.Container.GetConfig()
	fileDir := config.CommandLineArguments.AppBuildsDirectory
	fileName := payload.AppName + config.CommandLineArguments.CompressedBuildExtension
	filePath := fileDir + "/" + fileName

	// need to specify that this is a release build on remote update
	// this ensures that the dev release will be set to exists = true
	// prod ready builds will not be set to exists until after they are pushed
	app.ReleaseBuild = releaseBuild

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

	reader, err := sm.Container.Build(ctx, filePath, types.ImageBuildOptions{Tags: []string{payload.RegistryImageName.Dev}, Dockerfile: "Dockerfile"})
	if err != nil {
		var errorMessage string
		if errdefs.IsDockerfileCannotBeEmpty(err) {
			errorMessage = "The Dockerfile cannot be empty, please fill out your Dockerfile."
		} else if errdefs.IsDockerfileIsMissing(err) {
			errorMessage = "Could not find a Dockerfile, please create a Dockerfile in the root of your project."
		}

		if errorMessage != "" {
			messageErr := sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, errorMessage)
			if messageErr != nil {
				return messageErr
			}

			return err
		}

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

	buildResultMessage := "Image built successfully"
	if buildFailed {
		buildResultMessage = "Image build failed to complete"
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, fmt.Sprintf("%s", buildResultMessage))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.BUILT)
	if err != nil {
		return err
	}

	return nil
}
