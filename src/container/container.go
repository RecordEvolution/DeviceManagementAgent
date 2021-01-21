package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
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

var containerInstance Container
var containerType = DOCKER
var instanceLock sync.Once

// GetClientInstance ensures only one instance of a container client exists
func GetClientInstance() Container {
	instanceLock.Do(func() {
		switch containerType {
		case DOCKER:
			client, err := newDockerClient()
			if err != nil {
				panic(err)
			}
			containerInstance = &Docker{client: client}
		case LXC:
			{
				// TODO: implement LXC
			}
		}
	})
	return containerInstance
}

// SetContainerAPI sets the which api should be used (Docker, LXC). Docker by default.
// Has to be called before GetClientInstance
func SetContainerAPI(apiType Type) {
	containerType = apiType
}

func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

// ListImages lists all images available on the current device
func (docker *Docker) ListImages(ctx context.Context) ([]types.ImageSummary, error) {
	return docker.client.ImageList(ctx, types.ImageListOptions{All: true})
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

// Pull pulls a docker image from a registry
func (docker *Docker) Pull(ctx context.Context, imageName string) (io.ReadCloser, error) {
	closableReader, err := docker.client.ImagePull(ctx, imageName, types.ImagePullOptions{})

	if err != nil {
		return nil, err
	}

	return closableReader, nil
}

// Build builds a Docker image using a tarfile as context
func (docker *Docker) Build(ctx context.Context, pathToTar string, buildOptions interface{}) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(pathToTar)
	if err != nil {
		return nil, fmt.Errorf("Failed to open .tar file: %s", err)
	}

	if buildOptions == nil {
		buildOptions = types.ImageBuildOptions{}
	}

	castedOptions, ok := buildOptions.(types.ImageBuildOptions)
	if !ok {
		return nil, fmt.Errorf("Expected type types.ImageBuildOptions but got %T instead", buildOptions)
	}

	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, castedOptions)

	if err != nil {
		return nil, err
	}

	return buildResponse.Body, nil
}
