package container

import (
	"context"
	"io"
	"reagent/common"
	"reagent/config"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ContainerResult generic result for the container method ListContainers
type ContainerResult struct { // More fields can be added if needed, needs to be as generic as possible in case we want to use other container implementations
	ID      string
	Names   []string
	ImageID string
	Labels  map[string]string
	Status  string
	State   string
	Command string
}

type AuthConfig struct {
	Username string
	Password string
}

// ImageResult generic result for the container method ListImages
type ImageResult struct {
	Containers int64
	ID         string
	Labels     map[string]string
	Size       int64
	RepoTags   []string
}

// Container generic interface for a Container API
type Container interface {
	Login(ctx context.Context, username string, password string) error
	Build(ctx context.Context, pathToTar string, options types.ImageBuildOptions) (io.ReadCloser, error)
	CancelBuild(ctx context.Context, buildID string) error
	Stats(ctx context.Context, containerID string) (io.ReadCloser, error)
	Pull(ctx context.Context, imageName string, authConfig AuthConfig) (io.ReadCloser, error)
	Push(ctx context.Context, imageName string, authConfig AuthConfig) (io.ReadCloser, error)
	Run(ctx context.Context, cConfig container.Config, hConfig container.HostConfig, nConfig network.NetworkingConfig, containerName string) error
	RemoveImage(ctx context.Context, imageID string, options map[string]interface{}) error
	ListImages(ctx context.Context, options map[string]interface{}) ([]ImageResult, error)
	ListContainers(ctx context.Context, options common.Dict) ([]ContainerResult, error)
	GetConfig() config.Config
}
