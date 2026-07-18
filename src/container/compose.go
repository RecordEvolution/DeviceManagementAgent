package container

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reagent/common"
	"reagent/config"
	"reagent/safe"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Compose struct {
	Supported             bool
	config                *config.Config
	logStreamMap          map[string]*ComposeLog
	composeProcessesMap   map[string]context.CancelFunc
	composeProcessesMutex sync.Mutex
	logStreamMapMutex     sync.Mutex
}

type ComposeLog struct {
	channel chan string
	command *exec.Cmd
}

type ComposeBuildOptions struct {
}

type ComposeStatus struct {
	Command      string `json:"Command"`
	CreatedAt    string `json:"CreatedAt"`
	ExitCode     int    `json:"ExitCode"`
	Health       string `json:"Health"`
	ID           string `json:"ID"`
	Image        string `json:"Image"`
	Labels       string `json:"Labels"`
	LocalVolumes string `json:"LocalVolumes"`
	Mounts       string `json:"Mounts"`
	Name         string `json:"Name"`
	Names        string `json:"Names"`
	Networks     string `json:"Networks"`
	Ports        string `json:"Ports"`
	Project      string `json:"Project"`
	Publishers   []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
	RunningFor string `json:"RunningFor"`
	Service    string `json:"Service"`
	Size       string `json:"Size"`
	State      string `json:"State"`
	Status     string `json:"Status"`
}

type DockerCompose struct {
	Version  string             `json:"version"`
	Services map[string]Service `json:"services"`
}

type Service struct {
	Build       string   `json:"build"`
	Image       string   `json:"image"`
	Ports       []string `json:"ports"`
	Environment []string `json:"environment"`
}

func NewCompose(config *config.Config) Compose {
	supported := IsComposeSupported()

	return Compose{
		Supported:             supported,
		config:                config,
		logStreamMap:          make(map[string]*ComposeLog),
		composeProcessesMap:   make(map[string]context.CancelFunc),
		composeProcessesMutex: sync.Mutex{},
		logStreamMapMutex:     sync.Mutex{},
	}
}

func (c *Compose) ListImages(dockerCompose map[string]interface{}) ([]string, error) {
	services, ok := (dockerCompose["services"]).(map[string]interface{})
	if !ok {
		return nil, errors.New("failed to infer services")
	}

	images := make([]string, 0)
	for _, serviceInterface := range services {
		service, ok := (serviceInterface).(map[string]interface{})
		if !ok {
			return nil, errors.New("failed to infer service")
		}

		if service["image"] != nil {
			imageName := fmt.Sprint(service["image"])
			images = append(images, imageName)
		}
	}

	return images, nil
}

func (c *Compose) composeCommand(dockerComposePath string, providedArgs ...string) (chan string, *exec.Cmd, error) {
	return c.composeCommandContext(context.Background(), dockerComposePath, providedArgs...)
}

func (c *Compose) composeCommandContext(ctx context.Context, dockerComposePath string, providedArgs ...string) (chan string, *exec.Cmd, error) {
	finalArgs := []string{}
	finalArgs = append(finalArgs, "compose", "-f", dockerComposePath)
	finalArgs = append(finalArgs, providedArgs...)

	cmd := exec.CommandContext(ctx, "docker", finalArgs...)

	outputChan := make(chan string)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	cmd.Stderr = cmd.Stdout
	setPdeathsig(cmd)

	err = cmd.Start()
	if err != nil {
		return nil, nil, err
	}

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			outputChan <- text
		}

		close(outputChan)
	}()

	return outputChan, cmd, nil
}

func (c *Compose) Build(ctx context.Context, dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommandContext(ctx, dockerComposePath, "build")
}

func (c *Compose) RegisterBuildCancel(buildID string, cancel context.CancelFunc) {
	c.composeProcessesMutex.Lock()
	c.composeProcessesMap[buildID] = cancel
	c.composeProcessesMutex.Unlock()
}

func (c *Compose) UnregisterBuildCancel(buildID string) {
	c.composeProcessesMutex.Lock()
	delete(c.composeProcessesMap, buildID)
	c.composeProcessesMutex.Unlock()
}

func (c *Compose) CancelBuild(buildID string) error {
	c.composeProcessesMutex.Lock()
	cancel := c.composeProcessesMap[buildID]
	c.composeProcessesMutex.Unlock()
	if cancel == nil {
		return errors.New("no active compose build process found")
	}
	cancel()
	return nil
}

func (c *Compose) Push(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "push")
}

func (c *Compose) Pull(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "pull")
}

// PullIgnoreBuildable is for dev builds only, where images with a build section
// were just built locally and must not be pulled. Deployed apps must keep using
// Pull: their platform-rewritten compose files still contain build sections, but
// the images have to come from the registry. Requires compose >= v2.17.
func (c *Compose) PullIgnoreBuildable(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "pull", "--ignore-buildable")
}

func (c *Compose) Up(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "up", "--remove-orphans", "-d")
}

func (c *Compose) WaitForRunning(ctx context.Context, dockerComposePath string, pollingRate time.Duration) (<-chan struct{}, <-chan error) {
	errC := make(chan error, 1)
	runningC := make(chan struct{}, 1)

	safe.Go(func() {
		for {
			select {
			case <-ctx.Done():
				errC <- errors.New("waiting for running canceled")
				close(errC)
				close(runningC)
				return
			default:
				statuses, err := c.Status(dockerComposePath)
				if err != nil {
					errC <- err
					close(errC)
					close(runningC)
					return
				}

				if len(statuses) == 0 {
					continue
				}

				running, err := c.IsRunning(dockerComposePath)
				if err != nil {
					errC <- err
					close(errC)
					close(runningC)
					return
				}

				if running {
					runningC <- struct{}{}
					close(errC)
					close(runningC)
					return
				}

				for _, status := range statuses {
					if status.State == "exited" || status.State == "dead" {
						errC <- errors.New("the container has exited")
						close(errC)
						close(runningC)
						return
					}
				}

				time.Sleep(pollingRate)
			}
		}
	})

	return runningC, errC
}

func (c *Compose) IsRunning(dockerComposePath string) (bool, error) {
	statuses, err := c.Status(dockerComposePath)
	if err != nil {
		return false, err
	}

	allRunning := true
	for _, status := range statuses {
		if status.State != "running" {
			allRunning = false
		}
	}

	return allRunning, nil
}

func IsComposeSupported() bool {
	cmd := exec.Command("docker", "compose")
	_, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	return true
}

// RefreshSupport re-evaluates compose support. Supported is latched at
// construction, which can predate a late-starting daemon (Docker Desktop only
// starts at user login on Windows), so the daemon-wait path re-checks once
// Docker becomes available.
func (c *Compose) RefreshSupport() {
	c.Supported = IsComposeSupported()
}

func (c *Compose) Stop(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "stop")
}

func (c *Compose) Remove(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "rm", "-f")
}

func (c *Compose) Down(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "down", "-v")
}

// DownRemoveOrphans tears down the whole compose project — services in the
// file plus any orphan containers tagged with the same project name (services
// that were removed or renamed in a new compose file). Volumes are preserved
// (no `-v`) so user data survives the update.
func (c *Compose) DownRemoveOrphans(dockerComposePath string) (chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "down", "--remove-orphans")
}

func (c *Compose) LogsByContainerName(containerName string, tail uint64) (io.ReadCloser, error) {
	composeListEntry, err := c.List()
	if err != nil {
		return nil, err
	}

	var foundComposeEntry *ComposeListEntry
	for _, composeEntry := range composeListEntry {
		if composeEntry.Name == containerName {
			foundComposeEntry = &composeEntry
		}
	}

	if foundComposeEntry == nil {
		return nil, errors.New("compose entry not found")
	}

	output, err := exec.Command("docker", "compose", "-f", foundComposeEntry.ConfigFiles, "logs", "--tail", fmt.Sprint(tail)).CombinedOutput()
	if err != nil {
		return nil, err
	}

	reader := strings.NewReader(string(output))
	readCloser := io.NopCloser(reader)

	return readCloser, nil
}

func (c *Compose) Logs(dockerComposePath string, tail uint64) (io.ReadCloser, error) {
	output, err := exec.Command("docker", "compose", "-f", dockerComposePath, "logs", "--tail", fmt.Sprint(tail)).CombinedOutput()
	if err != nil {
		return nil, err
	}

	reader := strings.NewReader(string(output))
	readCloser := io.NopCloser(reader)

	return readCloser, nil
}

func (c *Compose) LogStream(dockerComposePath string) (chan string, error) {
	// c.logStreamMapMutex.Lock()
	// existingComposeLog := c.logStreamMap[dockerComposePath]

	// if existingComposeLog != nil {
	// 	err := existingComposeLog.command.Process.Kill()
	// 	if err != nil {
	// 		c.logStreamMapMutex.Unlock()
	// 		return nil, err
	// 	}

	// 	delete(c.logStreamMap, dockerComposePath)
	// }

	// c.logStreamMapMutex.Unlock()

	cmd := exec.Command("docker", "compose", "-f", dockerComposePath, "logs", "-f")
	cmdReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	cmd.Stderr = cmd.Stdout

	logChan := make(chan string)
	scanner := bufio.NewScanner(cmdReader)
	safe.Go(func() {
		for scanner.Scan() {
			chunk := scanner.Text()
			logChan <- chunk
		}

		close(logChan)
	})

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	// c.logStreamMapMutex.Lock()
	// c.logStreamMap[dockerComposePath] = &ComposeLog{channel: logChan, command: cmd}
	// c.logStreamMapMutex.Unlock()

	return logChan, nil
}

// parseComposePSOutput normalizes the output of `docker compose ps --format
// json` across compose versions: <= 2.20 prints one JSON array, newer versions
// print NDJSON (one object per line). Blank output yields an empty slice.
func parseComposePSOutput(output []byte) ([]ComposeStatus, error) {
	composeStatuses := []ComposeStatus{}

	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err != nil {
			return nil, err
		}

		value := bytes.TrimSpace(raw)
		if len(value) == 0 {
			continue
		}

		if value[0] == '[' {
			var batch []ComposeStatus
			err = json.Unmarshal(value, &batch)
			if err != nil {
				return nil, err
			}
			composeStatuses = append(composeStatuses, batch...)
		} else {
			var status ComposeStatus
			err = json.Unmarshal(value, &status)
			if err != nil {
				return nil, err
			}
			composeStatuses = append(composeStatuses, status)
		}
	}

	return composeStatuses, nil
}

func (c *Compose) Status(dockerComposePath string) ([]ComposeStatus, error) {
	if !c.Supported {
		log.Error().Err(errors.New("compose is not supported for this device")).Msg("Error while calling status")
		return []ComposeStatus{}, nil
	}

	cmd := exec.Command("docker", "compose", "-f", dockerComposePath, "ps", "-a", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		// A failing compose command (e.g. the project does not exist yet) must
		// yield an empty list rather than an error: WaitForRunning polls Status
		// and treats an empty result as "not up yet, keep waiting".
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Debug().Msgf("compose ps for %s exited non-zero: %s", dockerComposePath, strings.TrimSpace(string(exitErr.Stderr)))
			return []ComposeStatus{}, nil
		}
		return []ComposeStatus{}, err
	}

	return parseComposePSOutput(output)
}

// GetPublishedPorts returns the host ports the compose project currently
// publishes, keyed by "<service>|<targetPort>/<protocol>" (e.g. "web|80/tcp").
// An empty map when the project is not up.
func (c *Compose) GetPublishedPorts(dockerComposePath string) (map[string]uint64, error) {
	statuses, err := c.Status(dockerComposePath)
	if err != nil {
		return nil, err
	}

	published := make(map[string]uint64)
	for _, status := range statuses {
		for _, publisher := range status.Publishers {
			if publisher.PublishedPort == 0 {
				continue
			}
			key := PublishedPortKey(status.Service, uint64(publisher.TargetPort), publisher.Protocol)
			published[key] = uint64(publisher.PublishedPort)
		}
	}

	return published, nil
}

// PublishedPortKey builds the lookup key used by GetPublishedPorts.
func PublishedPortKey(service string, targetPort uint64, protocol string) string {
	if protocol == "" {
		protocol = "tcp"
	}
	return fmt.Sprintf("%s|%d/%s", service, targetPort, protocol)
}

type ComposeListEntry struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

func (c *Compose) HasComposeDir(appName string, stage common.Stage) bool {
	composeDir := c.config.CommandLineArguments.AppsComposeDir + "/" + appName
	if stage == common.DEV {
		composeDir = c.config.CommandLineArguments.AppsBuildDir + "/" + appName
	}

	_, err := os.Stat(composeDir)

	return err == nil
}

func (c *Compose) List() ([]ComposeListEntry, error) {
	if !c.Supported {
		log.Error().Err(errors.New("compose is not supported for this device")).Msg("Error while calling list")
		return []ComposeListEntry{}, nil
	}

	cmd := exec.Command("docker", "compose", "ls", "-a", "--format", "json")
	cmd.Stderr = cmd.Stdout

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var composeListEntries []ComposeListEntry
	err = json.Unmarshal([]byte(output), &composeListEntries)
	if err != nil {
		return nil, err
	}

	return composeListEntries, nil
}
