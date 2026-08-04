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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Compose struct {
	Supported bool
	config    *config.Config
	// binary is the CLI to invoke; empty means "docker". Swappable so tests can
	// exercise the output/exit-code plumbing without a daemon.
	binary                string
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

const (
	// composeTailLines is how many of a compose command's last output lines are
	// kept to explain a non-zero exit.
	composeTailLines = 25

	// composeStreamBuffer sizes the streamed output channel. Deep enough that a
	// consumer never realistically falls behind, so the drop below stays
	// theoretical.
	composeStreamBuffer = 4096

	// maxComposeLineBytes raises the per-line cap above bufio.Scanner's 64 KB
	// default. A longer line would otherwise end the scan silently, stop
	// draining the pipe and hang the CLI (and Wait) forever.
	maxComposeLineBytes = 1024 * 1024
)

// composeOutputTail keeps the last lines a compose command wrote. The CLI
// reports the actual reason for a failure (a port conflict, a missing image, a
// registry denial) on stderr and then exits 1, so without the tail a caller
// only ever sees the bare "exit status 1".
type composeOutputTail struct {
	mutex   sync.Mutex
	lines   []string
	dropped int
}

func (t *composeOutputTail) add(line string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if len(t.lines) == composeTailLines {
		t.lines = t.lines[1:]
	}
	t.lines = append(t.lines, line)
}

func (t *composeOutputTail) recordDropped() {
	t.mutex.Lock()
	t.dropped++
	t.mutex.Unlock()
}

func (t *composeOutputTail) String() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	lines := make([]string, 0, len(t.lines)+1)
	if t.dropped > 0 {
		lines = append(lines, fmt.Sprintf("(%d output line(s) dropped)", t.dropped))
	}
	for _, line := range t.lines {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "; ")
}

// ComposeError reports a `docker compose` invocation that exited non-zero,
// naming the subcommand and quoting the tail of what the CLI printed.
type ComposeError struct {
	Subcommand string
	Output     string
	Err        error
}

func (e *ComposeError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("docker compose %s failed: %s", e.Subcommand, e.Err)
	}
	return fmt.Sprintf("docker compose %s failed: %s: %s", e.Subcommand, e.Err, e.Output)
}

func (e *ComposeError) Unwrap() error {
	return e.Err
}

// ComposeCmd is a running `docker compose` invocation.
type ComposeCmd struct {
	cmd        *exec.Cmd
	subcommand string
	tail       *composeOutputTail
	drained    chan struct{}
}

// Wait blocks until the command has exited and all of its output has been
// read. A non-zero exit yields a *ComposeError carrying the CLI's own message.
func (cc *ComposeCmd) Wait() error {
	// exec.Cmd requires every read from StdoutPipe to have completed before
	// Wait: Wait closes the pipe, so waiting here is what keeps the tail (and
	// the streamed log lines) complete. The reader can never block, so this
	// cannot wedge — see composeCommandContext.
	<-cc.drained

	err := cc.cmd.Wait()
	if err == nil {
		return nil
	}

	return &ComposeError{Subcommand: cc.subcommand, Output: cc.tail.String(), Err: err}
}

func (c *Compose) composeCommand(dockerComposePath string, providedArgs ...string) (chan string, *ComposeCmd, error) {
	return c.composeCommandContext(context.Background(), dockerComposePath, providedArgs...)
}

// composeCommandContext starts a compose command and streams its combined
// output on the returned channel. run() is the variant for commands executed
// purely for their effect.
func (c *Compose) composeCommandContext(ctx context.Context, dockerComposePath string, providedArgs ...string) (chan string, *ComposeCmd, error) {
	return c.startComposeCommand(ctx, dockerComposePath, true, providedArgs...)
}

// run executes a compose command to completion, discarding its output except
// for the tail kept to explain a failure. Commands nothing consumes the stream
// of (stop, rm, down) must go through here: an unconsumed stream blocks its
// reader, which stops the pipe from being drained and deadlocks the CLI as
// soon as it outgrows the pipe buffer.
func (c *Compose) run(ctx context.Context, dockerComposePath string, providedArgs ...string) error {
	_, cmd, err := c.startComposeCommand(ctx, dockerComposePath, false, providedArgs...)
	if err != nil {
		return err
	}

	return cmd.Wait()
}

func (c *Compose) startComposeCommand(ctx context.Context, dockerComposePath string, streamed bool, providedArgs ...string) (chan string, *ComposeCmd, error) {
	finalArgs := []string{}
	finalArgs = append(finalArgs, "compose", "-f", dockerComposePath)
	finalArgs = append(finalArgs, providedArgs...)

	binary := c.binary
	if binary == "" {
		binary = "docker"
	}

	cmd := exec.CommandContext(ctx, binary, finalArgs...)

	var outputChan chan string
	if streamed {
		outputChan = make(chan string, composeStreamBuffer)
	}

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

	subcommand := "compose"
	if len(providedArgs) > 0 {
		subcommand = providedArgs[0]
	}

	composeCmd := &ComposeCmd{
		cmd:        cmd,
		subcommand: subcommand,
		tail:       &composeOutputTail{},
		drained:    make(chan struct{}),
	}

	go func() {
		defer close(composeCmd.drained)
		if streamed {
			defer close(outputChan)
		}

		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), maxComposeLineBytes)

		for scanner.Scan() {
			text := scanner.Text()
			composeCmd.tail.add(text)

			if !streamed {
				continue
			}

			// Never block: a stalled consumer must not stop the pipe from
			// being drained. Dropping a log line is recoverable, wedging the
			// transition is not — and the tail still carries the failure.
			select {
			case outputChan <- text:
			default:
				composeCmd.tail.recordDropped()
			}
		}
	}()

	return outputChan, composeCmd, nil
}

func (c *Compose) Build(ctx context.Context, dockerComposePath string) (chan string, *ComposeCmd, error) {
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

func (c *Compose) Push(dockerComposePath string) (chan string, *ComposeCmd, error) {
	return c.composeCommand(dockerComposePath, "push")
}

func (c *Compose) Pull(dockerComposePath string) (chan string, *ComposeCmd, error) {
	return c.composeCommand(dockerComposePath, "pull")
}

// PullContext is Pull bound to a cancelable context: canceling ctx kills the
// underlying `docker compose pull` process (see composeCommandContext), letting
// a caller abort a hung pull instead of blocking on cmd.Wait() forever. When
// service names are given, only those services are pulled (the install path
// pulls just the services whose images are missing locally, mirroring
// `compose up`'s implicit missing-only pull); without services the whole
// project is pulled.
func (c *Compose) PullContext(ctx context.Context, dockerComposePath string, services ...string) (chan string, *ComposeCmd, error) {
	args := append([]string{"pull"}, services...)
	return c.composeCommandContext(ctx, dockerComposePath, args...)
}

// PullIgnoreBuildable is for dev builds only, where images with a build section
// were just built locally and must not be pulled. Deployed apps must keep using
// Pull: their platform-rewritten compose files still contain build sections, but
// the images have to come from the registry. Requires compose >= v2.17.
func (c *Compose) PullIgnoreBuildable(dockerComposePath string) (chan string, *ComposeCmd, error) {
	return c.composeCommand(dockerComposePath, "pull", "--ignore-buildable")
}

func (c *Compose) Up(dockerComposePath string) (chan string, *ComposeCmd, error) {
	return c.composeCommand(dockerComposePath, "up", "--remove-orphans", "-d")
}

// UpNoBuild is Up for deployed apps. Their compose files keep the authored
// `build:` sections (see PullIgnoreBuildable), so when an image is missing
// from the registry `up` silently falls back to building it — from a context
// that only ever exists on a builder, never on a device. The build then fails
// on the missing context directory, burying the real cause ("image not
// found") under an unrelated lstat error. --no-build makes the pull failure
// itself be the failure.
func (c *Compose) UpNoBuild(dockerComposePath string) (chan string, *ComposeCmd, error) {
	return c.composeCommand(dockerComposePath, "up", "--remove-orphans", "-d", "--no-build")
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

func (c *Compose) Stop(dockerComposePath string) error {
	return c.run(context.Background(), dockerComposePath, "stop")
}

func (c *Compose) Remove(dockerComposePath string) error {
	return c.run(context.Background(), dockerComposePath, "rm", "-f")
}

func (c *Compose) Down(dockerComposePath string) error {
	return c.run(context.Background(), dockerComposePath, "down", "-v")
}

// DownRemoveOrphans tears down the whole compose project — services in the
// file plus any orphan containers tagged with the same project name (services
// that were removed or renamed in a new compose file). Volumes are preserved
// (no `-v`) so user data survives the update.
func (c *Compose) DownRemoveOrphans(dockerComposePath string) error {
	return c.run(context.Background(), dockerComposePath, "down", "--remove-orphans")
}

// DownRemoveOrphansContext is DownRemoveOrphans bound to a cancelable context;
// see PullContext.
func (c *Compose) DownRemoveOrphansContext(ctx context.Context, dockerComposePath string) error {
	return c.run(ctx, dockerComposePath, "down", "--remove-orphans")
}

// LogQuery bounds a one-shot log read.
//
// The zero value means "as much as the store still holds", which is what every
// caller predating time-windowed queries expects. Since and Until are RFC3339
// or Unix-seconds strings: resolve relative durations with common.ParseLogTime
// before filling them in, so the device's clock is applied exactly once and the
// same absolute window reaches both the Docker and the compose path.
type LogQuery struct {
	Tail       uint64
	Since      string
	Until      string
	Timestamps bool
}

// ComposeArgs renders the query as `docker compose logs` flags.
//
// --no-color is unconditional: compose only auto-disables colour when it
// detects a TTY, and an ANSI escape that survives into a log line is noise for
// every consumer we have.
func (q LogQuery) ComposeArgs() []string {
	args := []string{"logs", "--no-color"}
	if q.Tail > 0 {
		args = append(args, "--tail", strconv.FormatUint(q.Tail, 10))
	} else {
		args = append(args, "--tail", "all")
	}
	if q.Timestamps {
		args = append(args, "--timestamps")
	}
	if q.Since != "" {
		args = append(args, "--since", q.Since)
	}
	if q.Until != "" {
		args = append(args, "--until", q.Until)
	}
	return args
}

// DockerOptions renders the query as option keys for Container.Logs.
func (q LogQuery) DockerOptions() common.Dict {
	options := common.Dict{"follow": false, "stdout": true, "stderr": true}
	if q.Tail > 0 {
		options["tail"] = strconv.FormatUint(q.Tail, 10)
	} else {
		options["tail"] = "all"
	}
	if q.Timestamps {
		options["timestamps"] = true
	}
	if q.Since != "" {
		options["since"] = q.Since
	}
	if q.Until != "" {
		options["until"] = q.Until
	}
	return options
}

// LogsByContainerName reads the logs of a whole compose project, addressed by
// the agent's compose container name (`<stage>_<key>_<name>_compose`).
//
// Every service in the project is included, each line prefixed with its service
// name — for a multi-container app that prefix is the only thing identifying
// which service spoke, so it is deliberately kept. Output is CombinedOutput, so
// compose's own progress and warning text on stderr is interleaved with the
// container output.
func (c *Compose) LogsByContainerName(containerName string, query LogQuery) (io.ReadCloser, error) {
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

	return c.logs(foundComposeEntry.ConfigFiles, query)
}

func (c *Compose) Logs(dockerComposePath string, query LogQuery) (io.ReadCloser, error) {
	return c.logs(dockerComposePath, query)
}

func (c *Compose) logs(dockerComposePath string, query LogQuery) (io.ReadCloser, error) {
	binary := c.binary
	if binary == "" {
		binary = "docker"
	}

	args := append([]string{"compose", "-f", dockerComposePath}, query.ComposeArgs()...)

	output, err := exec.Command(binary, args...).CombinedOutput()
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
