package container

import (
	"bufio"
	"encoding/json"
	"errors"
	"os/exec"
	"reagent/safe"
	"strings"
	"sync"
	"time"
)

type Compose struct {
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

func NewCompose() Compose {
	return Compose{
		logStreamMap:          make(map[string]*ComposeLog),
		composeProcessesMap:   make(map[string]*exec.Cmd),
		composeProcessesMutex: sync.Mutex{},
		logStreamMapMutex:     sync.Mutex{},
	}
}

func (c *Compose) composeCommand(dockerComposePath string, providedArgs ...string) (chan string, chan error, *exec.Cmd, error) {
	finalArgs := []string{}
	finalArgs = append(finalArgs, "compose", "-f", dockerComposePath)
	finalArgs = append(finalArgs, providedArgs...)

	cmd := exec.Command("docker", finalArgs...)
	cmd.Env = append(cmd.Env, "COMPOSE_STATUS_STDOUT=1")

	stdoutChan := make(chan string)
	stderrChan := make(chan error)

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

		var stringBuilder strings.Builder
		for scanner.Scan() {
			text := scanner.Text()
			stringBuilder.WriteString(text)
			stringBuilder.WriteString("\n")
		}

		stringResult := stringBuilder.String()
		if len(stringResult) > 0 && strings.Contains(strings.ToLower(stringResult), "error") {
			stderrChan <- errors.New(stringResult)
		}

		close(stderrChan)
	}()

	return stdoutChan, stderrChan, cmd, nil
}

func (c *Compose) dockerCommand(providedArgs ...string) (chan string, chan error, *exec.Cmd, error) {
	cmd := exec.Command("docker", providedArgs...)
	cmd.Env = append(cmd.Env, "COMPOSE_STATUS_STDOUT=1")

	stdoutChan := make(chan string)
	stderrChan := make(chan error)

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

		var stringBuilder strings.Builder
		for scanner.Scan() {
			text := scanner.Text()
			stringBuilder.WriteString(text)
			stringBuilder.WriteString("\n")
		}

		stringResult := stringBuilder.String()
		if len(stringResult) > 0 && strings.Contains(strings.ToLower(stringResult), "error") {
			stderrChan <- errors.New(stringResult)
		}

		close(stderrChan)
	}()

	return stdoutChan, stderrChan, cmd, nil
}

func (c *Compose) Login(dockerRegistryURL string, username string, password string) (chan string, chan error, *exec.Cmd, error) {
	return c.dockerCommand("login", dockerRegistryURL, "-u", username, "-p", password)
}

func (c *Compose) Build(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "build")
}

func (c *Compose) Push(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "push")
}

func (c *Compose) Pull(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "pull")
}

func (c *Compose) Up(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "up", "-d")
}

func (c *Compose) WaitForRunning(dockerComposePath string, pollingRate time.Duration) (<-chan struct{}, <-chan error) {
	errC := make(chan error, 1)
	runningC := make(chan struct{}, 1)

	safe.Go(func() {
		for {

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

func (c *Compose) Stop(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "stop")
}

func (c *Compose) Remove(dockerComposePath string) (chan string, chan error, *exec.Cmd, error) {
	return c.composeCommand(dockerComposePath, "rm", "-f")
}

func (c *Compose) Logs(dockerComposePath string) (chan string, error) {
	c.logStreamMapMutex.Lock()
	existingComposeLog := c.logStreamMap[dockerComposePath]

	if existingComposeLog != nil {
		err := existingComposeLog.command.Process.Kill()
		if err != nil {
			c.logStreamMapMutex.Unlock()
			return nil, err
		}

		delete(c.logStreamMap, dockerComposePath)
	}

	c.logStreamMapMutex.Unlock()

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
			logChan <- scanner.Text()
		}

		close(logChan)
	})

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	c.logStreamMapMutex.Lock()
	c.logStreamMap[dockerComposePath] = &ComposeLog{channel: logChan, command: cmd}
	c.logStreamMapMutex.Unlock()

	return logChan, nil
}

func (c *Compose) Status(dockerComposePath string) ([]ComposeStatus, error) {
	cmd := exec.Command("docker", "compose", "-f", dockerComposePath, "ps", "-a", "--format", "\"{{ json . }}\"")
	cmd.Stderr = cmd.Stdout

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	strippedOutput := strings.TrimSpace(string(output))

	if len(strippedOutput) == 0 {
		return []ComposeStatus{}, nil
	}

	outputSplit := strings.Split(strippedOutput, "\n")

	composeStatuses := make([]ComposeStatus, 0)
	for _, jsonSplit := range outputSplit {

		var composeStatus ComposeStatus
		parsedJsonSplit := jsonSplit[1 : len(jsonSplit)-1]

		err := json.Unmarshal([]byte(parsedJsonSplit), &composeStatus)
		if err != nil {
			return nil, err
		}

		composeStatuses = append(composeStatuses, composeStatus)
	}

	return composeStatuses, nil
}
