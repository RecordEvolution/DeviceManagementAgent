package apps

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"

	"github.com/docker/docker/api/types"
)

func (sm *StateMachine) buildApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.DEV {
		return errors.New("can only build dev apps")
	}

	return sm.buildDevApp(payload, app, false)
}

func (sm *StateMachine) buildDevApp(payload common.TransitionPayload, app *common.App, releaseBuild bool) error {
	ctx := context.Background()

	sm.setState(app, common.REMOVED)

	config := sm.Container.GetConfig()
	fileDir := config.CommandLineArguments.AppBuildsDirectory
	fileName := payload.AppName + "." + config.CommandLineArguments.CompressedBuildExtension
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

	buildOptions := types.ImageBuildOptions{
		Tags:       []string{payload.RegistryImageName.Dev},
		Dockerfile: "Dockerfile",
		BuildID:    common.BuildDockerBuildID(app.AppKey, app.AppName),
	}

	topicForLogStream := payload.ContainerName.Dev
	if releaseBuild {
		topicForLogStream = payload.PublishContainerName
	}

	// Need to make sure we close this reader later on
	reader, err := sm.Container.Build(ctx, filePath, buildOptions)
	if err != nil {
		errorMessage := err.Error()
		if errdefs.IsDockerfileCannotBeEmpty(err) {
			errorMessage = "The Dockerfile cannot be empty, please fill out your Dockerfile"
		} else if errdefs.IsDockerfileIsMissing(err) {
			errorMessage = "Could not find a Dockerfile, please create a Dockerfile in the root of your project"
		}

		messageErr := sm.LogManager.Write(topicForLogStream, logging.BUILD, errorMessage)
		if messageErr != nil {
			return messageErr
		}

		return err
	}

	var buildMessage string
	streamErr := sm.LogManager.Stream(topicForLogStream, logging.BUILD, reader)
	if streamErr != nil {
		if errdefs.IsDockerBuildCanceled(streamErr) {
			buildMessage = "The build stream was canceled"
			writeErr := sm.LogManager.Write(topicForLogStream, logging.BUILD, buildMessage)
			if writeErr != nil {
				return writeErr
			}
			// a canceled build will transition to 'REMOVED|Another State' so no need to return the error
			return nil
		}

		return streamErr
	}

	buildMessage = "Image built successfully"
	err = sm.LogManager.Write(topicForLogStream, logging.BUILD, buildMessage)
	if err != nil {
		return err
	}

	return sm.setState(app, common.BUILT)
}
