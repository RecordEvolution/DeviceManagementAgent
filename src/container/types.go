package container

import (
	"context"
	"io"
	"reagent/api/common"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ListContainerResult generic result for the container method ListContainers
type ListContainerResult struct { // More fields can be added if needed, needs to be as generic as possible in case we want to use other container implementations
	ID      string
	Names   []string
	ImageID string
	Labels  map[string]string
	Status  string
	State   string
	Command string
}

// Container generic interface for a Container API
type Container interface {
	Login(ctx context.Context, username string, password string) (string, error)
	Build(ctx context.Context, pathToTar string, options types.ImageBuildOptions) (io.ReadCloser, error)
	Stats(ctx context.Context, containerID string) (io.ReadCloser, error)
	Pull(ctx context.Context, imageName string) (io.ReadCloser, error)
	Push(ctx context.Context, imageName string) (io.ReadCloser, error)
	Run(ctx context.Context, cConfig container.Config, hConfig container.HostConfig, nConfig network.NetworkingConfig, containerName string) error
	RemoveImage(ctx context.Context, imageID string) (string, error)
	ListImages(ctx context.Context, options interface{}) (string, error)
	ListContainers(ctx context.Context, options common.Dict) ([]ListContainerResult, error)
}
