package container

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reagent/api/common"
	"reagent/config"
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
			Containers: image.Containers,
			ID:         image.ID,
			Labels:     image.Labels,
			Size:       image.Size,
			RepoTags:   image.RepoTags,
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

// TODO: make more generic
//
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

// TODO: make more generic
//
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
