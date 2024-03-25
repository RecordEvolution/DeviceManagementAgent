package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/release"
	"reagent/tunnel"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) buildApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage != common.DEV {
		return errors.New("can only build dev apps")
	}

	return sm.buildDevApp(payload, app, false)
}

func (sm *StateMachine) generateDotEnvContents(config *config.Config, payload common.TransitionPayload, app *common.App) (string, error) {
	var dotEnvFileContents string

	systemDefaultVariables := buildDefaultEnvironmentVariables(config, app.Stage, app.AppKey)
	environmentVariables := buildProdEnvironmentVariables(systemDefaultVariables, payload.EnvironmentVariables)
	environmentTemplateDefaults := common.EnvironmentTemplateToStringArray(payload.EnvironmentTemplate)

	if payload.Stage == common.DEV {
		devEnvironmentVariables := append(systemDefaultVariables, environmentTemplateDefaults...)
		dotEnvFileContents = strings.Join(devEnvironmentVariables, "\n")
	} else {
		var missingDefaultEnvs []string
		for _, templateEnvString := range environmentTemplateDefaults {
			envStringSplit := strings.Split(templateEnvString, "=")
			environmentName := envStringSplit[0]

			found := false
			for _, envVariableString := range environmentVariables {
				if strings.Contains(envVariableString, environmentName) {
					found = true
				}
			}

			if !found {
				missingDefaultEnvs = append(missingDefaultEnvs, templateEnvString)
			}
		}

		var remotePortEnvs []string
		portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
		if err != nil {
			return "", err
		}

		for _, portRule := range portRules {
			if portRule.RemotePortEnvironment != "" {
				subdomain := tunnel.CreateSubdomain(tunnel.Protocol(portRule.Protocol), uint64(config.ReswarmConfig.DeviceKey), app.AppName, portRule.Port)
				tunnelID := tunnel.CreateTunnelID(subdomain, portRule.Protocol)
				tunnel := sm.StateObserver.AppManager.tunnelManager.Get(tunnelID)

				if tunnel != nil {
					portEnv := fmt.Sprintf("%s=%d", portRule.RemotePortEnvironment, tunnel.Config.RemotePort)
					remotePortEnvs = append(remotePortEnvs, portEnv)
				}
			}
		}

		dotEnvFileContents = strings.Join(append(environmentVariables, append(remotePortEnvs, missingDefaultEnvs...)...), "\n")
	}

	return dotEnvFileContents, nil
}

const DockerFileName = "docker-compose.json"
const DotEnvFileName = ".env-compose"

func (sm *StateMachine) SetupComposeFiles(payload common.TransitionPayload, app *common.App, updatingApp bool) (string, error) {
	config := sm.Container.GetConfig()

	isProd := payload.Stage == common.PROD
	targetDir := config.CommandLineArguments.AppsBuildDir
	if isProd {
		targetDir = config.CommandLineArguments.AppsComposeDir
	}

	targetAppDir := targetDir + "/" + app.AppName
	dockerComposeFilePath := targetAppDir + "/" + DockerFileName
	dotEnvFilePath := targetAppDir + "/" + DotEnvFileName

	dockerCompose := payload.DockerCompose
	if payload.NewDockerCompose != nil && updatingApp {
		dockerCompose = payload.NewDockerCompose
	}

	dockerCompose["name"] = common.BuildComposeContainerName(payload.Stage, app.AppKey, app.AppName)

	services, ok := (dockerCompose["services"]).(map[string]interface{})
	if !ok {
		return "", errors.New("failed to infer services")
	}

	for _, serviceInterface := range services {
		service, ok := (serviceInterface).(map[string]interface{})
		if !ok {
			return "", errors.New("failed to infer service")
		}

		service["env_file"] = DotEnvFileName
	}

	dockerComposeJSONString, err := json.Marshal(dockerCompose)
	if err != nil {
		return "", err
	}

	_, err = os.Stat(targetAppDir)
	if err != nil {
		err = os.MkdirAll(targetAppDir, os.ModePerm)
		if err != nil {
			return "", err
		}
	}

	dotEnvFileContents, err := sm.generateDotEnvContents(config, payload, app)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(dotEnvFilePath, []byte(dotEnvFileContents), os.ModePerm)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(dockerComposeFilePath, dockerComposeJSONString, os.ModePerm)
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

	compose := sm.Container.Compose()
	if !compose.Supported {
		message := "Docker Compose is not supported for this device"
		writeErr := sm.LogManager.Write(topicForLogStream, message)
		if writeErr != nil {
			return writeErr
		}
		return errdefs.DockerComposeNotSupported(errors.New("docker compose is not supported"))
	}

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, false)
	if err != nil {
		return err
	}

	_, buildStderr, buildCmd, err := compose.Build(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(buildStderr, topicForLogStream)
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

		_, err = sm.LogManager.StreamLogsChannel(pullStderr, topicForLogStream)
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

	// TODO: figure out context for build?
	reader, err := sm.Container.Build(context.Background(), appFilesTar, buildOptions)
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
