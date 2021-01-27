package apps

import (
	"fmt"
	"reagent/api/common"
	"reagent/config"
	"strings"
)

func AppStateToTransitionPayload(config *config.ReswarmConfig, app common.App) common.TransitionPayload {
	containerName := fmt.Sprintf("%s_%d_%s", app.Stage, app.AppKey, app.AppName)
	imageName := strings.ToLower(fmt.Sprintf("%s_%s_%d_%s", app.Stage, config.Architecture, app.AppKey, app.AppName))
	repositoryImageName := strings.ToLower(fmt.Sprintf("%s%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName))

	payload := common.TransitionPayload{
		RequestedState:      app.ManuallyRequestedState,
		Stage:               app.Stage,
		AppName:             app.AppName,
		AppKey:              uint64(app.AppKey),
		ImageName:           imageName,
		RepositoryImageName: repositoryImageName,
		CurrentState:        app.CurrentState,
		ContainerName:       containerName,
	}

	return payload
}
