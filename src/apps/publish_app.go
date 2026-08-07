package apps

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"strings"
	"time"
)

func (sm *StateMachine) publishApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.publishComposeApp(payload, app)
	}

	err := sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("Initializing publish process for %s...", app.AppName))
	if err != nil {
		return err
	}

	err = sm.buildDevApp(payload, app, true)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.PublishContainerName, "App build has finished, Starting to publish...")
	if err != nil {
		return err
	}

	prodImage := fmt.Sprintf("%s:%s", payload.RegistryImageName.Prod, payload.Version)
	tagContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	err = sm.Container.Tag(tagContext, payload.RegistryImageName.Dev, prodImage)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.PublishContainerName, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	pushOptions := container.PushOptions{
		AuthConfig: container.AuthConfig{
			Username: payload.RegisteryToken,
			Password: sm.Container.GetConfig().ReswarmConfig.Secret,
		},
		PushID: common.BuildDockerPushID(payload.AppKey, payload.AppName),
	}

	reader, err := sm.Container.Push(context.Background(), prodImage, pushOptions)
	if err != nil {
		return err
	}

	streamErr := sm.LogManager.StreamBlocking(payload.PublishContainerName, common.PUSH, reader)
	if streamErr != nil {
		if errdefs.IsDockerStreamCanceled(streamErr) {
			pushMessage := "The app release was canceled"
			writeErr := sm.LogManager.Write(payload.PublishContainerName, pushMessage)
			if writeErr != nil {
				return writeErr
			}
			// this error will not cause a failed state and is handled upstream
			return streamErr
		}

		return streamErr
	}

	err = sm.setState(app, common.PUBLISHED)
	if err != nil {
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.PublishContainerName)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) publishComposeApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("Initializing publish process for %s...", app.AppName))
	if err != nil {
		return err
	}

	err = sm.buildDevApp(payload, app, true)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.PublishContainerName, "App build has finished, Starting to publish...")
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	dockerComposePath, err := sm.SetupComposeFiles(payload, app, false)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	err = sm.HandleRegistryLoginsWithDefault(payload)
	if err != nil {
		writeErr := sm.LogManager.Write(payload.PublishContainerName, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	err = sm.rehostSourceImages(payload)
	if err != nil {
		if errdefs.IsDockerStreamCanceled(err) {
			// like a canceled push below: no failed state, handled upstream
			return err
		}
		writeErr := sm.LogManager.Write(payload.PublishContainerName, err.Error())
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	pushOutput, pushCmd, err := compose.Push(dockerComposePath)
	if err != nil {
		return err
	}

	_, err = sm.LogManager.StreamLogsChannel(pushOutput, payload.PublishContainerName)
	if err != nil {
		return err
	}

	err = pushCmd.Wait()
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHED)
	if err != nil {
		return err
	}

	err = sm.LogManager.ClearLogHistory(payload.PublishContainerName)
	if err != nil {
		return err
	}

	return nil
}

// registryHostOfImage returns the registry host of an image reference,
// following docker's own convention: the first path component is a registry
// host only when it contains '.' or ':' or is exactly "localhost"; every
// other reference lives on Docker Hub.
func registryHostOfImage(imageRef string) string {
	firstComponent, _, found := strings.Cut(imageRef, "/")
	if found && (strings.ContainsAny(firstComponent, ".:") || firstComponent == "localhost") {
		return firstComponent
	}
	return "docker.io"
}

// credentialForRegistryHost resolves the credential entry for a registry host,
// tolerating a trailing slash on the configured host (the appstore transfer's
// buildImageTransfer matches the same two spellings). A miss yields empty
// credentials, i.e. an anonymous pull.
func credentialForRegistryHost(credentials map[string]common.DockerCredential, host string) container.AuthConfig {
	cred, found := credentials[host]
	if !found {
		cred = credentials[host+"/"]
	}
	return container.AuthConfig{Username: cred.Username, Password: cred.Password}
}

// rehostSourceImages makes a release self-contained: f_create_release rewrites
// every service's image to a store-registry name and keeps the authored
// external ref of image-only services in the x-source-image extension field.
// `docker compose push` skips services without a build section, so those
// images are pulled from their source registry here (with the app's
// docker_credentials — carried only by build/publish payloads), retagged to
// the store name and pushed explicitly. After this, installs never touch the
// external registry or need the app's credentials. Releases created before
// the rewrite carry no x-source-image fields, making this a no-op for them.
func (sm *StateMachine) rehostSourceImages(payload common.TransitionPayload) error {
	services, ok := (payload.DockerCompose["services"]).(map[string]interface{})
	if !ok {
		return errors.New("failed to infer services")
	}

	config := sm.Container.GetConfig()

	for serviceName, serviceInterface := range services {
		service, ok := (serviceInterface).(map[string]interface{})
		if !ok {
			return errors.New("failed to infer service")
		}

		sourceImage, _ := service["x-source-image"].(string)
		if sourceImage == "" {
			continue
		}

		targetImage, _ := service["image"].(string)
		if targetImage == "" {
			return fmt.Errorf("service %s declares x-source-image but no image", serviceName)
		}

		err := sm.LogManager.Write(payload.PublishContainerName, fmt.Sprintf("Re-hosting image %s of service %s as %s...", sourceImage, serviceName, targetImage))
		if err != nil {
			return err
		}

		pullOptions := container.PullOptions{
			AuthConfig: credentialForRegistryHost(payload.DockerCredentials, registryHostOfImage(sourceImage)),
			PullID:     common.BuildDockerPullID(payload.AppKey, payload.AppName),
		}

		pullReader, err := sm.Container.Pull(context.Background(), sourceImage, pullOptions)
		if err != nil {
			return err
		}

		streamErr := sm.LogManager.StreamBlocking(payload.PublishContainerName, common.PULL, pullReader)
		if streamErr != nil {
			return streamErr
		}

		tagContext, cancelTag := context.WithTimeout(context.Background(), time.Second*30)
		err = sm.Container.Tag(tagContext, sourceImage, targetImage)
		cancelTag()
		if err != nil {
			return err
		}

		pushOptions := container.PushOptions{
			AuthConfig: container.AuthConfig{
				Username: payload.RegisteryToken,
				Password: config.ReswarmConfig.Secret,
			},
			PushID: common.BuildDockerPushID(payload.AppKey, payload.AppName),
		}

		pushReader, err := sm.Container.Push(context.Background(), targetImage, pushOptions)
		if err != nil {
			return err
		}

		streamErr = sm.LogManager.StreamBlocking(payload.PublishContainerName, common.PUSH, pushReader)
		if streamErr != nil {
			return streamErr
		}
	}

	return nil
}
