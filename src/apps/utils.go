package apps

import (
	"fmt"
	"reagent/api/common"
	"reagent/config"
)

func AppStateToTransitionPayload(config *config.ReswarmConfig, app common.App) TransitionPayload {
	containerName := fmt.Sprintf("%s_%d_%s", app.Stage, app.AppKey, app.AppName)
	imageName := fmt.Sprintf("%s_%s_%d_%s", app.Stage, config.Architecture, app.AppKey, app.AppName)
	repositoryImageName := fmt.Sprintf("%s/%s%s", config.DockerRegistryURL, config.DockerMainRepository, imageName)

	payload := TransitionPayload{
		RequestedState:      app.ManuallyRequestedState,
		Stage:               app.Stage,
		AppName:             app.AppName,
		AppKey:              uint64(app.AppKey),
		ImageName:           imageName,
		RepositoryImageName: repositoryImageName,
		ContainerName:       containerName,
	}

	return payload
}
