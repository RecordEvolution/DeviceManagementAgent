package container

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type Docker struct {
	client *client.Client
}

type Lxc struct {
}

type ContainerType string

const (
	DOCKER ContainerType = "docker"
	LXC    ContainerType = "lxc"
)

const defaultRegistry = "https://index.docker.io/v1/"

var containerInstance Container
var containerType = DOCKER
var instanceLock sync.Once

// GetClientInstance ensures only one instance of a Container Client exists
func GetClientInstance() Container {
	instanceLock.Do(func() {
		switch containerType {
		case DOCKER:
			client, err := newDockerClient()
			if err != nil {
				panic(err)
			}
			containerInstance = Docker{client: client}
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
func SetContainerAPI(apiType ContainerType) {
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
func (docker Docker) Login(ctx context.Context, username string, password string, registryURL string) (string, error) {
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
		log.Fatalln("Docker.Login() failed to login with credentials:", username, password, "to", registryURL, err)
		return "", err
	}

	if !strings.Contains(authOkBody.Status, "Login Succeeded") {
		return "", fmt.Errorf("Login failed with status: %s", authOkBody.Status)
	}

	log.Println(authOkBody.Status)
	return authOkBody.Status, nil
}

// Pull pulls a docker image from a registry
func (docker *Docker) Pull(ctx context.Context, imageName string) (io.ReadCloser, error) {
	closableReader, err := docker.client.ImagePull(ctx, imageName, types.ImagePullOptions{})

	if err != nil {
		log.Fatal("Docker.Pull() failed to image with name:", imageName, err)
		return nil, err
	}

	return closableReader, nil
}

// Build builds a Docker image using a tarfile as context
func (docker Docker) Build(ctx context.Context, pathToTar string, buildOptions interface{}) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(pathToTar)

	if buildOptions == nil {
		buildOptions = types.ImageBuildOptions{}
	}

	castedOptions, ok := buildOptions.(types.ImageBuildOptions)
	if !ok {
		return nil, fmt.Errorf("Expected type types.ImageBuildOptions but got %T instead", buildOptions)
	}

	if err != nil {
		log.Fatal("Docker.Build() failed to open tar with path:", pathToTar, err)
		return nil, err
	}

	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, castedOptions)

	if err != nil {
		log.Fatal("Docker.Build() failed to build docker image", err)
		return nil, err
	}

	return buildResponse.Body, nil
}
