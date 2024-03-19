package container

import (
	"bufio"
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
)

type Compose struct {
	config                *config.Config
	logStreamMap          map[string]*ComposeLog
	composeProcessesMap   map[string]*exec.Cmd
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
	return Compose{
		config:                config,
		logStreamMap:          make(map[string]*ComposeLog),
		composeProcessesMap:   make(map[string]*exec.Cmd),
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

func (c *Compose) composeCommand(dockerComposePath string, providedArgs ...string) (chan string, chan string, *exec.Cmd, error) {
	finalArgs := []string{}
	finalArgs = append(finalArgs, "compose", "-f", dockerComposePath)
	finalArgs = append(finalArgs, providedArgs...)

	cmd := exec.Command("docker", finalArgs...)

	stdoutChan := make(chan string)
	stderrChan := make(chan string)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, nil, nil, err
	}

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			stdoutChan <- text
		}

		close(stdoutChan)
	}()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			text := scanner.Text()
			stderrChan <- text
		}

		close(stderrChan)
	}()

	return stdoutChan, stderrChan, cmd, nil
}

func (c *Compose) dockerCommand(providedArgs ...string) (chan string, chan string, *exec.Cmd, error) {
	cmd := exec.Command("docker", providedArgs...)

	stdoutChan := make(chan string)
	stderrChan := make(chan string)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, nil, nil, err
	}

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			stdoutChan <- text
		}

		close(stdoutChan)
	}()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			text := scanner.Text()
			stderrChan <- text
		}

		close(stderrChan)
	}()

	return stdoutChan, stderrChan, cmd, nil
}

func (c *Compose) Login(dockerRegistryURL string, username string, password string) (chan string, chan string, *exec.Cmd, error) {
	return c.dockerCommand("login", dockerRegistryURL, "-u", username, "-p", password)
}

func (c *Compose) Build(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "build")
}

func (c *Compose) Push(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "push")
}

func (c *Compose) Pull(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "pull")
}

func (c *Compose) Up(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "up", "-d")
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

func (c *Compose) Stop(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "stop")
}

func (c *Compose) Remove(dockerComposePath string) (chan string, chan string, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "rm", "-f")
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

func (c *Compose) Status(dockerComposePath string) ([]ComposeStatus, error) {
	statusCommand := fmt.Sprintf("docker compose -f %s ps -a --format json | jq -sc '.[] | if type==\"array\" then .[] else . end' | jq -s", dockerComposePath)
	cmd := exec.Command("bash", "-c", statusCommand)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var composeStatuses []ComposeStatus
	err = json.Unmarshal(output, &composeStatuses)
	if err != nil {
		return nil, err
	}

	return composeStatuses, nil
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
