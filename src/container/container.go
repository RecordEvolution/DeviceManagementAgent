package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Docker container implentation using the Docker API
type Docker struct {
	client *client.Client
}

// Lxc container implentation using the LXC API
type Lxc struct {
}

// Type type of container instance that is set. (Docker, LXC, ...)
type Type string

const (
	// DOCKER container type
	DOCKER Type = "docker"
	// LXC container type
	LXC Type = "lxc"
)

const defaultRegistry = "https://index.docker.io/v1/"

// For now stick only with Docker as implementation
var containerInstance *Docker

// var containerType = DOCKER
var instanceLock sync.Once

// GetClientInstance creates or gets an instance of the set container API. (Docker by default)
func GetClientInstance() (*Docker, error) {
	var initalizationError error
	instanceLock.Do(func() {
		// switch containerType {
		// case DOCKER:
		client, err := newDockerClient()
		if err != nil {
			initalizationError = err
		} else {
			containerInstance = &Docker{client: client}
		}
		// case LXC:
		// 	{
		// 		initalizationError = fmt.Errorf("Not yet implemented")
		// 	}
		// }
	})

	if initalizationError != nil {
		return nil, initalizationError
	}

	return containerInstance, nil
}

// // SetContainerAPI sets the which api should be used (Docker, LXC). Docker by default.
// // Has to be called before GetClientInstance
// func SetContainerAPI(apiType Type) {
// 	containerType = apiType
// }

func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

// ListImages lists all images available on the host.
func (docker *Docker) ListImages(ctx context.Context, options interface{}) (string, error) {
	var rOptions types.ImageListOptions
	if options == nil {
		rOptions = types.ImageListOptions{}
	} else {
		cOptions, ok := options.(types.ImageListOptions)
		if !ok {
			return "", fmt.Errorf("Excepted types.ImageListOptions{} but got %T instead", options)
		}
		rOptions = cOptions
	}

	imageList, err := docker.client.ImageList(ctx, rOptions)
	if err != nil {
		return "", err
	}

	byteArr, err := json.Marshal(&imageList)
	if err != nil {
		return "", err
	}

	return string(byteArr), nil
}

// Login allows user to authenticate with a specific registry
func (docker *Docker) Login(ctx context.Context, username string, password string, registryURL string) (string, error) {
	if registryURL == "" {
		registryURL = defaultRegistry
	}

	authConfig := types.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: registryURL,
	}

	authOkBody, err := docker.client.RegistryLogin(ctx, authConfig)
	if err != nil {
		return "", err
	}

	if !strings.Contains(authOkBody.Status, "Login Succeeded") {
		return "", fmt.Errorf("Login failed with status: %s", authOkBody.Status)
	}

	return authOkBody.Status, nil
}

// Pull pulls a container image from a registry
func (docker *Docker) Pull(ctx context.Context, imageName string) (io.ReadCloser, error) {
	return docker.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
}

// Push pushes a container image to a registry
func (docker *Docker) Push(ctx context.Context, imageName string) (io.ReadCloser, error) {
	return docker.client.ImagePush(ctx, imageName, types.ImagePushOptions{})
}

// Stats gets the stats of a specific container
func (docker *Docker) Stats(ctx context.Context, containerID string) (io.ReadCloser, error) {
	stats, err := docker.client.ContainerStats(ctx, containerID, true)
	if err != nil {
		return nil, err
	}
	return stats.Body, nil
}

// Run creates and starts a specific container
func (docker *Docker) Run(ctx context.Context,
	cConfig container.Config,
	hConfig container.HostConfig,
	nConfig network.NetworkingConfig,
	containerName string,
) error {
	resp, err := docker.client.ContainerCreate(ctx, &cConfig, &hConfig, &nConfig, nil, containerName)
	if err != nil {
		return err
	}

	if err := docker.client.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	return nil
}

// RemoveImage removes an image from the host
func (docker *Docker) RemoveImage(ctx context.Context, imageID string) (string, error) {
	response, err := docker.client.ImageRemove(ctx, imageID, types.ImageRemoveOptions{Force: true, PruneChildren: true})
	if err != nil {
		return "", err
	}

	byteArr, err := json.Marshal(&response)
	if err != nil {
		return "", err
	}
	return string(byteArr), nil
}

// Build builds a Docker image using a tarfile as context
func (docker *Docker) Build(ctx context.Context, pathToTar string, buildOptions types.ImageBuildOptions) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(pathToTar)
	if err != nil {
		return nil, fmt.Errorf("Failed to open .tar file: %s", err)
	}

	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, buildOptions)

	if err != nil {
		return nil, err
	}

	return buildResponse.Body, nil
}
