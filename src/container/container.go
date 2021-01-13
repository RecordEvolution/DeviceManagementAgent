package container

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
)

//  "github.com/docker/docker/client"
//  "github.com/docker/docker/api/types"
//  "github.com/docker/docker/api/types/container"

//type Image interface {
//  pull(name string) bool
//  tag(tag string)
//  remove(hash string) bool
//  push() bool
//}

// TODO separate interface to different file
type Container interface {
	Pull(image string) bool
	Push(image string) bool
	Remove(imageid string) bool
	Tag(name string)
	ListImage() []string
	Build(image string) bool
	Run(image string, name string) bool
	// ...
}

type Docker struct {
	client      *client.Client
	registryURL string
}

func New(registryURL string) *Docker {
	client, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	return &Docker{client: client, registryURL: registryURL}
}

func (docker *Docker) ListImages(ctx context.Context) ([]types.ImageSummary, error) {
	return docker.client.ImageList(ctx, types.ImageListOptions{All: true})
}

func (docker Docker) Login(ctx context.Context, username string, password string) (registry.AuthenticateOKBody, error) {
	authConfig := types.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: docker.registryURL,
	}
	return docker.client.RegistryLogin(ctx, authConfig)
}

func (docker Docker) Pull(image string) bool {
	return true
}

func (docker Docker) Push(image string) bool {
	return true
}

func (docker Docker) Build(image string) bool {
	return true
}

// ....
