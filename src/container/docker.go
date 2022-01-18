package container

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/safe"
	"reagent/system"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
)

// Docker container implentation using the Docker API

type DockerPull struct {
	BuildID string
	Stream  io.ReadCloser
}
type DockerStream struct {
	StreamID string
	Stream   io.ReadCloser
}

type Docker struct {
	client         *client.Client
	config         *config.Config
	activeStreams  map[string]*DockerStream
	streamMapMutex sync.Mutex
}

func NewDocker(config *config.Config) (*Docker, error) {
	client, err := newDockerClient()
	activeBuilds := make(map[string]*DockerStream)

	if err != nil {
		return nil, err
	}
	return &Docker{client: client, config: config, activeStreams: activeBuilds}, nil
}

// For now stick only with Docker as implementation
func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

func (docker *Docker) ListenForContainerEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	eventFilters := filters.NewArgs()
	eventFilters.Add("type", "container")

	return docker.client.Events(ctx, types.EventsOptions{Filters: eventFilters})
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

		exitCode := int64(-1)
		if cont.State == "exited" {
			exitCode, err = common.ParseExitCodeFromContainerStatus(cont.Status)
		}

		dict := ContainerResult{
			ID:      cont.ID,
			Names:   cont.Names,
			ImageID: cont.ImageID,
			Command: cont.Command,
			Labels:  cont.Labels,
			State:   cont.State,
			Status:  cont.Status,
		}

		if exitCode != -1 {
			dict.ExitCode = exitCode
		}

		listOfDict = append(listOfDict, dict)
	}

	return listOfDict, nil
}

func (docker *Docker) GetContainer(ctx context.Context, containerName string) (types.Container, error) {
	filters := filters.NewArgs()
	filters.Add("name", containerName)
	containers, err := docker.client.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return types.Container{}, err
	}

	if len(containers) > 0 {
		return containers[0], nil
	}

	return types.Container{}, errdefs.ContainerNotFound(errors.New("container not found"))
}

func (docker *Docker) RemoveContainerByName(ctx context.Context, containerName string, options map[string]interface{}) error {
	container, err := docker.GetContainer(ctx, containerName)

	if err != nil {
		return err
	}

	return docker.RemoveContainerByID(ctx, container.ID, options)
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

	err := docker.client.ContainerRemove(ctx, containerID, optionStruct)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			return errdefs.ContainerNotFound(err)
		}
		if strings.Contains(err.Error(), "is already in progress") {
			return errdefs.ContainerRemovalAlreadyInProgress(err)
		}
		return err
	}

	return nil
}

func (docker *Docker) StopContainerByID(ctx context.Context, containerID string, timeout time.Duration) error {
	return docker.client.ContainerStop(ctx, containerID, &timeout)
}

func (docker *Docker) StopContainerByName(ctx context.Context, containerName string, timeout time.Duration) error {
	container, err := docker.GetContainer(ctx, containerName)
	if err != nil {
		return err
	}

	return docker.client.ContainerStop(ctx, container.ID, &timeout)
}

func (docker *Docker) GetImages(ctx context.Context, fullImageName string) ([]ImageResult, error) {
	filters := filters.NewArgs()
	filters.Add("reference", fullImageName)

	options := types.ImageListOptions{Filters: filters}

	imagesResult, err := docker.client.ImageList(ctx, options)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			return []ImageResult{}, errdefs.ImageNotFound(err)
		}
		return []ImageResult{}, err
	}

	if len(imagesResult) == 0 {
		return []ImageResult{}, nil
	}

	var images []ImageResult
	for i := range imagesResult {
		image := imagesResult[i]

		images = append(images, ImageResult{
			Created:     image.Created,
			Containers:  image.Containers,
			SharedSize:  image.SharedSize,
			VirtualSize: image.VirtualSize,
			ID:          image.ID,
			Labels:      image.Labels,
			Size:        image.Size,
			RepoTags:    image.RepoTags,
		})
	}

	return images, nil
}

func (docker *Docker) GetImage(ctx context.Context, fullImageName string, tag string) (ImageResult, error) {

	filters := filters.NewArgs()
	fullImageNameWithTag := fmt.Sprintf("%s:%s", fullImageName, tag)
	filters.Add("reference", fullImageNameWithTag)

	options := types.ImageListOptions{Filters: filters}

	images, err := docker.client.ImageList(ctx, options)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			return ImageResult{}, errdefs.ImageNotFound(err)
		}
		return ImageResult{}, err
	}

	if len(images) == 0 {
		return ImageResult{}, errdefs.ImageNotFound(fmt.Errorf("no image found with name: %s:%s", fullImageName, tag))
	}

	image := images[0]

	return ImageResult{
		Created:     image.Created,
		Containers:  image.Containers,
		SharedSize:  image.SharedSize,
		VirtualSize: image.VirtualSize,
		ID:          image.ID,
		Labels:      image.Labels,
		Size:        image.Size,
		RepoTags:    image.RepoTags,
	}, nil
}

func (docker *Docker) PruneImages(ctx context.Context, options common.Dict) error {
	filters := filters.NewArgs()

	if options["all"] != nil {
		all, ok := options["all"].(bool)
		if !ok {
			return errors.New("all value for container prune is not a boolean")
		}
		if all {
			filters.Add("dangling", "false")
		}
	}

	_, err := docker.client.ImagesPrune(ctx, filters)
	return err
}

func (docker *Docker) PruneSystem(ctx context.Context) (string, error) {
	cmd := exec.Command("docker", "system", "prune", "-a", "-f")
	cmd.Stderr = cmd.Stdout
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
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

func (docker *Docker) GetConfig() *config.Config {
	return docker.config
}

type PullOptions struct {
	AuthConfig AuthConfig
	PullID     string
}

type PushOptions struct {
	AuthConfig AuthConfig
	PushID     string
}

// Pull pulls a container image from a registry
func (docker *Docker) Pull(ctx context.Context, imageName string, options PullOptions) (io.ReadCloser, error) {
	dockerAuthConfig := types.AuthConfig{
		Username: options.AuthConfig.Username,
		Password: options.AuthConfig.Password,
	}

	encodedJSON, err := json.Marshal(dockerAuthConfig)
	if err != nil {
		return nil, err
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	reader, err := docker.client.ImagePull(ctx, imageName, types.ImagePullOptions{RegistryAuth: authStr})
	if err != nil {
		if reader != nil {
			reader.Close()
		}

		return nil, err
	}

	pullID := options.PullID
	pullStream := DockerStream{
		Stream:   reader,
		StreamID: pullID,
	}

	if pullID != "" {
		docker.streamMapMutex.Lock()
		docker.activeStreams[pullID] = &pullStream
		docker.streamMapMutex.Unlock()
	}

	return reader, nil
}

// Push pushes a container image to a registry
func (docker *Docker) Push(ctx context.Context, imageName string, options PushOptions) (io.ReadCloser, error) {
	dockerAuthConfig := types.AuthConfig{
		Username: options.AuthConfig.Username,
		Password: options.AuthConfig.Password,
	}

	encodedJSON, err := json.Marshal(dockerAuthConfig)
	if err != nil {
		return nil, err
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	reader, err := docker.client.ImagePush(ctx, imageName, types.ImagePushOptions{RegistryAuth: authStr})
	if err != nil {
		if reader != nil {
			reader.Close()
		}

		return nil, err
	}

	pushID := options.PushID
	pushStream := DockerStream{
		Stream:   reader,
		StreamID: pushID,
	}

	if pushID != "" {
		docker.streamMapMutex.Lock()
		docker.activeStreams[pushID] = &pushStream
		docker.streamMapMutex.Unlock()
	}

	return reader, nil
}

func (docker *Docker) Logs(ctx context.Context, containerName string, options common.Dict) (io.ReadCloser, error) {
	containerOptions := types.ContainerLogsOptions{}
	stdoutKw := options["stdout"]
	stderrKw := options["stderr"]
	followKw := options["follow"]
	tailKw := options["tail"]

	if stdoutKw != nil {
		stdout, ok := stdoutKw.(bool)
		if ok {
			containerOptions.ShowStdout = stdout
		}
	}

	if stderrKw != nil {
		stderr, ok := stderrKw.(bool)
		if ok {
			containerOptions.ShowStderr = stderr
		}
	}

	if followKw != nil {
		follow, ok := followKw.(bool)
		if ok {
			containerOptions.Follow = follow
		}
	}

	if tailKw != nil {
		tail, ok := tailKw.(string)
		if ok {
			containerOptions.Tail = tail
		}
	}

	reader, err := docker.client.ContainerLogs(ctx, containerName, containerOptions)
	if err != nil {
		if reader != nil {
			reader.Close()
		}

		if strings.Contains(err.Error(), "No such container") {
			return nil, errdefs.ContainerNotFound(err)
		}
		if strings.Contains(err.Error(), "container which is dead or marked for removal") {
			return nil, errdefs.ContainerNotFound(err)
		}
		return nil, err
	}

	return reader, nil
}

func (docker *Docker) WaitForContainerByName(ctx context.Context, containerName string, condition container.WaitCondition) (int64, error) {
	container, err := docker.GetContainer(ctx, containerName)
	if err != nil {
		return -1, err
	}

	statusChan, errChan := docker.client.ContainerWait(ctx, container.ID, condition)
	select {
	case err := <-errChan:
		if strings.Contains(err.Error(), "No such container") {
			return -1, errdefs.ContainerNotFound(err)
		}
		return -1, err
	case status := <-statusChan:
		return status.StatusCode, nil
	}
}

func (docker *Docker) WaitForContainerByID(ctx context.Context, containerID string, condition container.WaitCondition) (int64, error) {
	statusChan, errChan := docker.client.ContainerWait(ctx, containerID, condition)
	select {
	case err := <-errChan:
		if strings.Contains(err.Error(), "No such container") {
			return -1, errdefs.ContainerNotFound(err)
		}
		return -1, err
	case status := <-statusChan:
		return status.StatusCode, nil
	}
}

func (docker *Docker) Attach(ctx context.Context, containerName string, shell string) (HijackedResponse, error) {
	attachOpts := types.ContainerAttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
		Logs:   false,
		Stdin:  true,
	}

	response, err := docker.client.ContainerAttach(ctx, containerName, attachOpts)
	if err != nil {
		return HijackedResponse{}, err
	}

	return HijackedResponse{
		Conn:   response.Conn,
		Reader: response.Reader,
	}, nil
}

func (docker *Docker) ResizeExecContainer(ctx context.Context, execID string, dimension TtyDimension) error {
	return docker.client.ContainerExecResize(ctx, execID, types.ResizeOptions{
		Height: dimension.Height,
		Width:  dimension.Width,
	})
}

func (docker *Docker) ExecAttach(ctx context.Context, containerName string, shell string) (HijackedResponse, error) {
	execConfig := types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		AttachStdin:  true,
		Tty:          true,
		Cmd:          []string{shell},
	}
	execOptions := types.ExecStartCheck{
		Tty: true,
	}

	container, err := docker.GetContainer(ctx, containerName)
	if err != nil {
		return HijackedResponse{}, err
	}

	respID, err := docker.client.ContainerExecCreate(ctx, container.ID, execConfig)
	if err != nil {
		return HijackedResponse{}, err
	}

	response, err := docker.client.ContainerExecAttach(ctx, respID.ID, execOptions)
	if err != nil {
		return HijackedResponse{}, err
	}

	return HijackedResponse{
		Conn:   response.Conn,
		Reader: response.Reader,
		ExecID: respID.ID,
	}, nil
}

func (docker *Docker) ExecCommand(ctx context.Context, containerName string, cmd []string) (HijackedResponse, error) {
	execConfig := types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Tty:          false,
		Cmd:          cmd,
	}
	execOptions := types.ExecStartCheck{}

	container, err := docker.GetContainer(ctx, containerName)
	if err != nil {
		return HijackedResponse{}, err
	}

	respID, err := docker.client.ContainerExecCreate(ctx, container.ID, execConfig)
	if err != nil {
		return HijackedResponse{}, err
	}

	response, err := docker.client.ContainerExecAttach(ctx, respID.ID, execOptions)
	if err != nil {
		return HijackedResponse{}, err
	}

	return HijackedResponse{
		Conn:   response.Conn,
		Reader: response.Reader,
		ExecID: respID.ID,
	}, nil
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

		if strings.Contains(err.Error(), "No such image") {
			return "", errdefs.ImageNotFound(err)
		}

		return "", err
	}

	return resp.ID, nil
}

func (docker *Docker) GetContainers(ctx context.Context) ([]types.Container, error) {
	options := types.ContainerListOptions{All: true}
	return docker.client.ContainerList(ctx, options)
}

func (docker *Docker) GetContainerState(ctx context.Context, containerName string) (ContainerState, error) {
	res, err := docker.client.ContainerInspect(ctx, containerName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return ContainerState{}, errdefs.ContainerNotFound(err)
		}
		return ContainerState{}, err
	}

	state := res.State
	return ContainerState{
		Status:     state.Status,
		Running:    state.Running,
		Paused:     state.Paused,
		Restarting: state.Restarting,
		OOMKilled:  state.OOMKilled,
		Dead:       state.Dead,
		Pid:        state.Pid,
		ExitCode:   state.ExitCode,
		Error:      state.Error,
		StartedAt:  state.StartedAt,
		FinishedAt: state.FinishedAt,
	}, nil
}

func (docker *Docker) PollContainerState(ctx context.Context, containerID string, pollingRate time.Duration) (<-chan ContainerState, <-chan error) {
	errC := make(chan error, 1)
	stateC := make(chan ContainerState, 1)

	safe.Go(func() {
		for {
			state, err := docker.GetContainerState(ctx, containerID)
			if err != nil {
				if strings.Contains(err.Error(), "no such container") {
					err = errdefs.ContainerNotFound(err)
				}

				errC <- err
				close(errC)
				close(stateC)
				return
			}

			stateC <- state
			time.Sleep(pollingRate)
		}
	})

	return stateC, errC
}

// WaitForRunning will poll a container's status at a given interval until the running state is achieved.
// If the container fails to start with an 'exited' or 'dead' status, it will throw an error
func (docker *Docker) WaitForRunning(ctx context.Context, containerID string, pollingRate time.Duration) (<-chan struct{}, <-chan error) {
	errC := make(chan error, 1)
	runningC := make(chan struct{}, 1)

	safe.Go(func() {
		for {
			state, err := docker.GetContainerState(ctx, containerID)
			if err != nil {
				errC <- err
				close(errC)
				close(runningC)
				return
			}

			if state.Running {
				runningC <- struct{}{}
				close(errC)
				close(runningC)
				return
			}

			if state.Status == "exited" || state.Status == "dead" {
				errC <- errors.New("container failed to start")
				close(errC)
				close(runningC)
				return
			}

			time.Sleep(pollingRate)
		}
	})

	return runningC, errC
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

func (docker *Docker) RemoveImagesByName(ctx context.Context, imageName string, options map[string]interface{}) error {
	filters := filters.NewArgs()
	filters.Add("reference", imageName)

	images, err := docker.client.ImageList(ctx, types.ImageListOptions{Filters: filters})
	if err != nil {
		return err
	}

	for _, image := range images {
		for _, repoTag := range image.RepoTags {
			if repoTag != "" {
				err := docker.RemoveImage(ctx, repoTag, options)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (docker *Docker) RemoveImageByName(ctx context.Context, imageName string, tag string, options map[string]interface{}) error {
	filters := filters.NewArgs()

	filters.Add("reference", fmt.Sprintf("%s:%s", imageName, tag))

	images, err := docker.client.ImageList(ctx, types.ImageListOptions{Filters: filters})
	if err != nil {
		return err
	}

	if len(images) == 0 {
		return fmt.Errorf("no image was found with name: %s:%s", imageName, tag)
	}

	if len(images) > 1 {
		return fmt.Errorf("multiple images were found with name: %s:%s", imageName, tag)
	}

	image := images[0]

	return docker.RemoveImage(ctx, image.ID, options)
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

type Ping struct {
	APIVersion     string
	OSType         string
	Experimental   bool
	BuilderVersion string
}

func (docker *Docker) WaitForDaemon(retryTimeoutParam ...time.Duration) error {
	var retryTimeout time.Duration

	if len(retryTimeoutParam) == 0 {
		retryTimeout = time.Second * 5
	} else {
		retryTimeout = retryTimeoutParam[0]
	}

	timeoutTimer := time.Now()
	for {
		_, err := docker.Ping(context.Background())
		if err != nil {
			log.Debug().Msg("Ping to Docker Daemon failed, retrying...")
			time.Sleep(time.Millisecond * 100)
		} else {
			break
		}

		if time.Since(timeoutTimer) > retryTimeout {
			return errors.New("maxium retry timeout for docker daemon ping was exceeded")
		}
	}

	return nil
}

func (docker *Docker) Ping(ctx context.Context) (Ping, error) {
	pingRes, err := docker.client.Ping(ctx)
	if err != nil {
		return Ping{}, err
	}

	return Ping{
		APIVersion:     pingRes.APIVersion,
		OSType:         pingRes.OSType,
		Experimental:   pingRes.Experimental,
		BuilderVersion: string(pingRes.BuilderVersion),
	}, nil
}

func (docker *Docker) CancelStream(streamID string) error {
	docker.streamMapMutex.Lock()
	activeStreamEntry := docker.activeStreams[streamID]
	docker.streamMapMutex.Unlock()

	if activeStreamEntry == nil {
		return errors.New("no active stream was found")
	}

	stream := activeStreamEntry.Stream
	if stream != nil {
		err := stream.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (docker *Docker) CancelAllStreams() error {
	docker.streamMapMutex.Lock()
	defer docker.streamMapMutex.Unlock()

	for _, stream := range docker.activeStreams {
		if stream != nil {
			err := stream.Stream.Close()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (docker *Docker) Tag(ctx context.Context, source string, target string) error {
	return docker.client.ImageTag(ctx, source, target)
}

// Build builds a Docker image using a tarfile as context
func (docker *Docker) Build(ctx context.Context, compressedBuildFilesPath string, options types.ImageBuildOptions) (io.ReadCloser, error) {
	dockerBuildContext, err := os.Open(compressedBuildFilesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errdefs.DockerBuildFilesNotFound(err)
		}
		return nil, err
	}

	options.Platform = system.GetPlatformString()
	buildResponse, err := docker.client.ImageBuild(ctx, dockerBuildContext, options)
	if err != nil {
		if buildResponse.Body != nil {
			buildResponse.Body.Close()
		}

		if strings.Contains(err.Error(), "the Dockerfile (Dockerfile) cannot be empty") {
			return nil, errdefs.DockerfileCannotBeEmpty(err)
		}
		if strings.Contains(err.Error(), "Cannot locate specified Dockerfile: Dockerfile") {
			return nil, errdefs.DockerfileIsMissing(err)
		}
		return nil, err
	}

	reader := buildResponse.Body
	if options.BuildID != "" {
		docker.streamMapMutex.Lock()
		docker.activeStreams[options.BuildID] = &DockerStream{
			StreamID: options.BuildID,
			Stream:   reader,
		}
		docker.streamMapMutex.Unlock()
	}

	return reader, nil
}
