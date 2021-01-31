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

func (sm *StateMachine) buildApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	if payload.Stage == common.DEV {
		sm.buildDevApp(payload, app, errorChannel)
	}
}

func (sm *StateMachine) buildDevApp(payload common.TransitionPayload, app *common.App, errorChannel chan error) {
	ctx := context.Background() // TODO: store context in memory for build cancellation

	config := sm.Container.GetConfig()
	fileDir := config.CommandLineArguments.AppBuildsDirectory
	fileName := payload.AppName + config.CommandLineArguments.CompressedBuildExtension
	filePath := fileDir + "/" + fileName

	exists, _ := filesystem.FileExists(filePath)
	if !exists {
		sm.setState(app, common.FAILED)
		err := fmt.Errorf("build files do not exist on path %s", filePath)
		errorChannel <- err
		return
	}

	err := sm.setState(app, common.BUILDING)
	fmt.Printf("%+v \n", sm.appStates)

	if err != nil {
		errorChannel <- err
		return
	}

	reader, err := sm.Container.Build(ctx, filePath, types.ImageBuildOptions{Tags: []string{payload.RepositoryImageName}, Dockerfile: "Dockerfile"})

	if err != nil {
		errorChannel <- err
		return
	}

	err = sm.LogManager.Stream(payload.ContainerName, logging.BUILD, reader)

	buildFailed := false
	if err != nil {
		if errdefs.IsBuildFailed(err) {
			buildFailed = true
		} else {
			errorChannel <- err
			return
		}
	}

	app.ManuallyRequestedState = common.PRESENT
	err = sm.setState(app, common.PRESENT)
	if err != nil {
		errorChannel <- err
		return
	}

	buildResultMessage := "Image built successfully"
	if buildFailed {
		buildResultMessage = "Image build failed to complete"
	}

	err = sm.LogManager.Write(payload.ContainerName, logging.BUILD, fmt.Sprintf("%s", buildResultMessage))
	if err != nil {
		errorChannel <- err
		return
	}

	errorChannel <- nil
}
