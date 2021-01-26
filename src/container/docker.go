package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reagent/api/common"
	"reagent/fs"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Docker container implentation using the Docker API
type Docker struct {
	client *client.Client
	config *fs.ReswarmConfig
}

func NewDocker(config *fs.ReswarmConfig) (*Docker, error) {
	client, err := newDockerClient()
	if err != nil {
		return nil, err
	}
	return &Docker{client: client, config: config}, nil
}

// For now stick only with Docker as implementation
func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

func (docker *Docker) ListContainers(ctx context.Context, options common.Dict) ([]common.Dict, error) {
	listOptions := types.ContainerListOptions{}

	if options != nil {
		quietKw := options["quiet"]
		allKw := options["all"]
		sizeKw := options["size"]
		latestKw := options["latest"]
		sinceKw := options["since"]
		beforeKw := options["before"]
		limitKw := options["limit"]
		filtersKw := options["filters"]

		quiet, ok := quietKw.(bool)
		if ok {
			listOptions.Quiet = quiet
		}
		all, ok := allKw.(bool)
		if ok {
			listOptions.All = all
		}
		size, ok := sizeKw.(bool)
		if ok {
			listOptions.Size = size
		}
		latest, ok := latestKw.(bool)
		if ok {
			listOptions.Latest = latest
		}
		since, ok := sinceKw.(string)
		if ok {
			listOptions.Since = since
		}
		before, ok := beforeKw.(string)
		if ok {
			listOptions.Before = before
		}
		limit, ok := limitKw.(int)
		if ok {
			listOptions.Limit = limit
		}
		filters, ok := filtersKw.(filters.Args)
		if ok {
			listOptions.Filters = filters
		}
	}

	cList, err := docker.client.ContainerList(ctx, listOptions)
	if err != nil {
		return nil, err
	}

	listOfDict := make([]common.Dict, 0)
	for _, cont := range cList {
		dict := common.Dict{
			"id":              cont.ID,
			"names":           cont.Names,
			"imageID":         cont.ImageID,
			"command":         cont.Command,
			"created":         cont.Created,
			"ports":           cont.Ports,
			"sizeRw":          cont.SizeRw,
			"sizeRootFs":      cont.SizeRootFs,
			"labels":          cont.Labels,
			"state":           cont.State,
			"status":          cont.Status,
			"hostConfig":      cont.HostConfig,
			"networkSettings": cont.NetworkSettings,
			"mounts":          cont.Mounts,
		}
		listOfDict = append(listOfDict, dict)
	}

	return listOfDict, nil
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
func (docker *Docker) Login(ctx context.Context, username string, password string) (string, error) {
	authConfig := types.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: docker.config.DockerRegistryURL,
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

func (docker *Docker) GetConfig() *fs.ReswarmConfig {
	return docker.config
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
func (docker *Docker) Build(ctx context.Context, pathToTar string, options types.ImageBuildOptions) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(pathToTar)
	if err != nil {
		return nil, fmt.Errorf("Failed to open .tar file: %s", err)
	}

	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, options)

	if err != nil {
		return nil, err
	}

	return buildResponse.Body, nil
}
