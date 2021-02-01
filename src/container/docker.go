package container

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Docker container implentation using the Docker API
type Docker struct {
	client *client.Client
	config config.Config
}

func NewDocker(config config.Config) (*Docker, error) {
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

// TODO: simplifiy option parsing
//
// ListContainers lists containers on the host
func (docker *Docker) ListContainers(ctx context.Context, options common.Dict) ([]ContainerResult, error) {
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

	listOfDict := make([]ContainerResult, 0)
	for _, cont := range cList {
		dict := ContainerResult{
			ID:      cont.ID,
			Names:   cont.Names,
			ImageID: cont.ImageID,
			Command: cont.Command,
			Labels:  cont.Labels,
			State:   cont.State,
			Status:  cont.Status,
		}
		listOfDict = append(listOfDict, dict)
	}

	return listOfDict, nil
}

func (docker *Docker) GetContainerID(ctx context.Context, containerName string) (string, error) {
	filters := filters.NewArgs()
	filters.Add("name", containerName)
	containers, err := docker.client.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return "", err
	}

	if len(containers) > 0 {
		return containers[0].ID, nil
	}

	return "", errdefs.ContainerNotFound(errors.New("container not found"))
}

func (docker *Docker) RemoveContainerByName(ctx context.Context, containerName string, options map[string]interface{}) error {
	id, err := docker.GetContainerID(ctx, containerName)

	if err != nil {
		return err
	}

	return docker.RemoveContainerByID(ctx, id, options)
}

func (docker *Docker) RemoveContainerByID(ctx context.Context, containerID string, options map[string]interface{}) error {
	optionStruct := types.ContainerRemoveOptions{}

	removeVolumesKw := options["removeVolumes"]
	removeLinksKw := options["removeLinks"]
	forceKw := options["force"]

	if removeVolumesKw != nil {
		removeVolumes, ok := removeVolumesKw.(bool)
		if ok {
			optionStruct.RemoveVolumes = removeVolumes
		}
	}

	if removeLinksKw != nil {
		removeLinks, ok := removeLinksKw.(bool)
		if ok {
			optionStruct.RemoveLinks = removeLinks
		}
	}

	if forceKw != nil {
		force, ok := forceKw.(bool)
		if ok {
			optionStruct.Force = force
		}
	}

	return docker.client.ContainerRemove(ctx, containerID, optionStruct)
}

func (docker *Docker) StopContainerByID(ctx context.Context, containerID string, timeout int64) error {
	return docker.client.ContainerStop(ctx, containerID, (*time.Duration)(&timeout))
}

func (docker *Docker) StopContainerByName(ctx context.Context, containerName string, timeout int64) error {
	id, err := docker.GetContainerID(ctx, containerName)
	if err != nil {
		return err
	}

	return docker.client.ContainerStop(ctx, id, (*time.Duration)(&timeout))
}

// ListImages lists all images available on the host.
func (docker *Docker) ListImages(ctx context.Context, options map[string]interface{}) ([]ImageResult, error) {
	allKw := options["all"]

	rOptions := types.ImageListOptions{}

	if allKw != nil {
		all, ok := allKw.(bool)
		if !ok {
			return nil, fmt.Errorf("Invalid type for 'all' option")
		}
		rOptions.All = all
	}

	imageList, err := docker.client.ImageList(ctx, rOptions)
	if err != nil {
		return nil, err
	}

	imageResults := make([]ImageResult, 0)
	for _, image := range imageList {
		imageResult := ImageResult{
			Created:     image.Created,
			Containers:  image.Containers,
			SharedSize:  image.SharedSize,
			VirtualSize: image.VirtualSize,
			ID:          image.ID,
			Labels:      image.Labels,
			Size:        image.Size,
			RepoTags:    image.RepoTags,
		}
		imageResults = append(imageResults, imageResult)
	}

	return imageResults, nil
}

// Login allows user to authenticate with a specific registry
func (docker *Docker) Login(ctx context.Context, username string, password string) error {
	authConfig := types.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: docker.config.ReswarmConfig.DockerRegistryURL,
	}

	authOkBody, err := docker.client.RegistryLogin(ctx, authConfig)
	if err != nil {
		return err
	}

	if !strings.Contains(authOkBody.Status, "Login Succeeded") {
		return fmt.Errorf("Login failed with status: %s", authOkBody.Status)
	}

	return nil
}

func (docker *Docker) GetConfig() config.Config {
	return docker.config
}

// Pull pulls a container image from a registry
func (docker *Docker) Pull(ctx context.Context, imageName string, authConfig AuthConfig) (io.ReadCloser, error) {
	dockerAuthConfig := types.AuthConfig{
		Username: authConfig.Username,
		Password: authConfig.Password,
	}
	encodedJSON, err := json.Marshal(dockerAuthConfig)
	if err != nil {
		return nil, err
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)
	return docker.client.ImagePull(ctx, imageName, types.ImagePullOptions{RegistryAuth: authStr})
}

// Push pushes a container image to a registry
func (docker *Docker) Push(ctx context.Context, imageName string, authConfig AuthConfig) (io.ReadCloser, error) {
	dockerAuthConfig := types.AuthConfig{
		Username: authConfig.Username,
		Password: authConfig.Password,
	}
	encodedJSON, err := json.Marshal(dockerAuthConfig)
	if err != nil {
		return nil, err
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)
	return docker.client.ImagePush(ctx, imageName, types.ImagePushOptions{RegistryAuth: authStr})
}

// Stats gets the stats of a specific container
func (docker *Docker) Stats(ctx context.Context, containerID string) (io.ReadCloser, error) {
	stats, err := docker.client.ContainerStats(ctx, containerID, true)
	if err != nil {
		return nil, err
	}
	return stats.Body, nil
}

func (docker *Docker) WaitForContainer(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.ContainerWaitOKBody, <-chan error) {
	return docker.client.ContainerWait(ctx, containerID, condition)
}

func (docker *Docker) CreateContainer(ctx context.Context,
	cConfig container.Config,
	hConfig container.HostConfig,
	nConfig network.NetworkingConfig,
	containerName string) (string, error) {

	resp, err := docker.client.ContainerCreate(ctx, &cConfig, &hConfig, &nConfig, nil, containerName)
	if err != nil {
		if strings.Contains(err.Error(), "is already in use by container") {
			return "", errdefs.ContainerNameAlreadyInUse(err)
		}

		return "", err
	}

	return resp.ID, nil
}

// TODO: make more generic
//
// StartContainer creates and starts a specific container
func (docker *Docker) StartContainer(ctx context.Context, containerID string) error {
	if err := docker.client.ContainerStart(ctx, containerID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	return nil
}

// RemoveImage removes an image from the host
func (docker *Docker) RemoveImage(ctx context.Context, imageID string, options map[string]interface{}) error {
	forceKw := options["force"]
	pruneChildrenKw := options["pruneChildren"]

	rOptions := types.ImageRemoveOptions{}
	if forceKw != nil {
		force, ok := forceKw.(bool)
		if !ok {
			return fmt.Errorf("Invalid type for 'force' option")
		}
		rOptions.Force = force
	}

	if pruneChildrenKw != nil {
		pruneChildren, ok := pruneChildrenKw.(bool)
		if !ok {
			return fmt.Errorf("Invalid type for 'pruneChilderen' option")
		}
		rOptions.PruneChildren = pruneChildren
	}

	_, err := docker.client.ImageRemove(ctx, imageID, rOptions)
	if err != nil {
		return err
	}

	return nil
}

func (docker *Docker) CancelBuild(ctx context.Context, buildID string) error {
	return docker.client.BuildCancel(ctx, buildID)
}

func (docker *Docker) Tag(ctx context.Context, source string, target string) error {
	return docker.client.ImageTag(ctx, source, target)
}

// TODO: make more generic
//
// Build builds a Docker image using a tarfile as context
func (docker *Docker) Build(ctx context.Context, pathToTar string, options types.ImageBuildOptions) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(pathToTar)
	if err != nil {
		return nil, fmt.Errorf("Failed to open compressed build file: %s", err)
	}

	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, options)

	if err != nil {
		return nil, err
	}

	return buildResponse.Body, nil
}
