package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reagent/common"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/release"

	"github.com/docker/docker/api/types"
	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) buildApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.DEV {
		return errors.New("can only build dev apps")
	}

	return sm.buildDevApp(payload, app, false)
}

func (sm *StateMachine) WriteDockerComposeFile(payload common.TransitionPayload, app *common.App, updatingApp bool) (string, error) {
	dockerFileName := "docker-compose.json"
	config := sm.Container.GetConfig()

	isProd := payload.Stage == common.PROD
	targetDir := config.CommandLineArguments.AppsBuildDir
	if isProd {
		targetDir = config.CommandLineArguments.AppsComposeDir
	}

	targetAppDir := targetDir + "/" + app.AppName
	dockerComposeFilePath := targetAppDir + "/" + dockerFileName

	dockerCompose := payload.DockerCompose
	if payload.NewDockerCompose != nil && updatingApp {
		dockerCompose = payload.NewDockerCompose
	}

	dockerComposeJSONString, err := json.Marshal(dockerCompose)
	if err != nil {
		return "", err
	}

	if isProd {
		_, err = os.Stat(targetAppDir)
		if err != nil {
			err = os.MkdirAll(targetAppDir, os.ModePerm)
			if err != nil {
				return "", err
			}
		}
	}

	err = os.WriteFile(dockerComposeFilePath, dockerComposeJSONString, 0755)
	if err != nil {
		return "", err
	}

	return dockerComposeFilePath, nil
}

func (sm *StateMachine) buildDevComposeApp(payload common.TransitionPayload, app *common.App, releaseBuild bool) error {
	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.REMOVED)
	if err != nil {
		return err
	}

	config := sm.Container.GetConfig()
	buildsDir := config.CommandLineArguments.AppsBuildDir
	fileName := payload.AppName + "." + config.CommandLineArguments.CompressedBuildExtension
	appFilesTar := buildsDir + "/" + fileName
	targetAppDir := buildsDir + "/" + app.AppName

	_, err = os.Stat(targetAppDir)
	if err == nil {
		err := os.RemoveAll(targetAppDir)
		if err != nil {
			return err
		}
	}

	err = filesystem.ExtractTarGz(appFilesTar, targetAppDir)
	if err != nil {
		return err
	}

	app.ReleaseBuild = releaseBuild
	topicForLogStream := payload.ContainerName.Dev
	if releaseBuild {
		topicForLogStream = payload.PublishContainerName
	}

	err = sm.LogManager.Write(topicForLogStream, "Starting image build...")
	if err != nil {
		return err
	}

	err = sm.setState(app, common.BUILDING)
	if err != nil {
		return err
	}

	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()
	_, buildStderr, buildCmd, err := compose.Build(dockerComposePath)
	if err != nil {
		return err
	}

	err = sm.LogManager.StreamChannel(topicForLogStream, common.BUILD, buildStderr)
	if err != nil {
		return err
	}

	err = buildCmd.Wait()
	if err != nil {
		return err
	}

	if !releaseBuild {
		_, pullStderr, pullCmd, err := compose.Pull(dockerComposePath)
		if err != nil {
			return err
		}

		err = sm.LogManager.StreamChannel(topicForLogStream, common.BUILD, pullStderr)
		if err != nil {
			return err
		}

		err = pullCmd.Wait()
		if err != nil {
			return err
		}
	}

	buildMessage := "Compose Image built successfully"
	err = sm.LogManager.Write(topicForLogStream, buildMessage)
	if err != nil {
		return err
	}

	return sm.setState(app, common.BUILT)
}

func (sm *StateMachine) buildDevApp(payload common.TransitionPayload, app *common.App, releaseBuild bool) error {
	ctx := context.Background()

	if payload.DockerCompose != nil {
		return sm.buildDevComposeApp(payload, app, releaseBuild)
	}

	err := sm.LogManager.ClearLogHistory(payload.ContainerName.Dev)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.REMOVED)
	if err != nil {
		return err
	}

	config := sm.Container.GetConfig()
	buildsDir := config.CommandLineArguments.AppsBuildDir
	fileName := payload.AppName + "." + config.CommandLineArguments.CompressedBuildExtension
	appFilesTar := buildsDir + "/" + fileName

	dockerFileName := "Dockerfile"
	buildArch := release.GetBuildArch()
	archSpecificDockerfile := fmt.Sprintf("Dockerfile.%s", buildArch)
	_, err = filesystem.ReadFileInTgz(appFilesTar, archSpecificDockerfile)
	if err == nil {
		dockerFileName = archSpecificDockerfile
	}

	// need to specify that this is a release build on remote update
	// this ensures that the dev release will be set to exists = true
	// prod ready builds will not be set to exists until after they are pushed
	app.ReleaseBuild = releaseBuild
	buildOptions := types.ImageBuildOptions{
		Tags:       []string{payload.RegistryImageName.Dev},
		Dockerfile: dockerFileName,
		BuildID:    common.BuildDockerBuildID(app.AppKey, app.AppName),
	}

	topicForLogStream := payload.ContainerName.Dev
	if releaseBuild {
		topicForLogStream = payload.PublishContainerName
	}

	err = sm.LogManager.Write(topicForLogStream, "Starting image build...")
	if err != nil {
		return err
	}

	err = sm.setState(app, common.BUILDING)
	if err != nil {
		return err
	}

	reader, err := sm.Container.Build(ctx, appFilesTar, buildOptions)
	if err != nil {
		errorMessage := err.Error()
		if errdefs.IsDockerfileCannotBeEmpty(err) {
			errorMessage = "The Dockerfile cannot be empty, please fill out your Dockerfile"
		} else if errdefs.IsDockerfileIsMissing(err) {
			errorMessage = "Could not find a Dockerfile, please create a Dockerfile in the root of your project"
		} else if errdefs.IsDockerBuildFilesNotFound(err) {
			errorMessage = "Build files for app not found: " + err.Error()
		}

		log.Debug().Msgf("building failed sending following message to user %s", errorMessage)

		messageErr := sm.LogManager.Write(topicForLogStream, errorMessage)
		if messageErr != nil {
			return messageErr
		}

		return err
	}

	var buildMessage string
	streamErr := sm.LogManager.StreamBlocking(topicForLogStream, common.BUILD, reader)
	if streamErr != nil {
		if errdefs.IsDockerStreamCanceled(streamErr) {
			buildMessage = "The build stream was canceled"
			writeErr := sm.LogManager.Write(topicForLogStream, buildMessage)
			if writeErr != nil {
				return writeErr
			}
			// this error will not cause a failed state and is handled upstream
			return streamErr
		}

		return streamErr
	}

	buildMessage = "Image built successfully"
	err = sm.LogManager.Write(topicForLogStream, buildMessage)
	if err != nil {
		return err
	}

	return sm.setState(app, common.BUILT)
}
