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
		errorMessage := err.Error()
		if errdefs.IsDockerfileCannotBeEmpty(err) {
			errorMessage = "The Dockerfile cannot be empty, please fill out your Dockerfile"
		} else if errdefs.IsDockerfileIsMissing(err) {
			errorMessage = "Could not find a Dockerfile, please create a Dockerfile in the root of your project"
		}

		messageErr := sm.LogManager.Write(payload.ContainerName.Dev, logging.BUILD, errorMessage)
		if messageErr != nil {
			return messageErr
		}

		return err
	}

	topicForLogStream := payload.ContainerName.Dev
	if releaseBuild {
		topicForLogStream = payload.PublishContainerName
	}

	err = sm.LogManager.Stream(topicForLogStream, logging.BUILD, reader)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(topicForLogStream, logging.BUILD, fmt.Sprintf("%s", "Image built successfully"))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.BUILT)
	if err != nil {
		return err
	}

	return nil
}
