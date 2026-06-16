//go:build integration

package container

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"reagent/testutil/builders"

	dtypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationImage is a tiny public image used for the round-trip. hello-world
// prints a message and exits 0, so it exercises create/start/wait/logs without
// needing a long-running process or network access at runtime.
const integrationImage = "hello-world:latest"

// newIntegrationDocker builds the REAL docker client and skips the test when no
// reachable daemon is present, so 'just test-integration' stays green on hosts
// without docker.
func newIntegrationDocker(t *testing.T) *Docker {
	t.Helper()

	docker, err := NewDocker(builders.DefaultTestConfig())
	if err != nil {
		t.Skipf("skipping: cannot construct docker client: %v", err)
	}

	// WaitForDaemon pings the daemon; a short timeout keeps the skip fast when
	// the socket is absent or unreachable.
	if err := docker.WaitForDaemon(2 * time.Second); err != nil {
		t.Skipf("skipping: docker daemon not available: %v", err)
	}

	return docker
}

// drain fully reads and closes a stream returned by the docker client. Pull/Logs
// return readers that must be drained for the underlying operation to complete.
func drain(t *testing.T, r io.ReadCloser) {
	t.Helper()
	require.NotNil(t, r)
	defer r.Close()
	_, err := io.Copy(io.Discard, r)
	require.NoError(t, err)
}

func TestIntegrationDockerPing(t *testing.T) {
	docker := newIntegrationDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ping, err := docker.Ping(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, ping.APIVersion, "daemon should report an API version")
	assert.NotEmpty(t, ping.OSType, "daemon should report an OS type")
}

func TestIntegrationDockerListImagesAndContainers(t *testing.T) {
	docker := newIntegrationDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// ListImages should succeed against a live daemon (result may be empty).
	images, err := docker.ListImages(ctx, map[string]interface{}{"all": false})
	require.NoError(t, err)
	assert.NotNil(t, images)

	// ListContainers with all=true should also succeed.
	containers, err := docker.ListContainers(ctx, map[string]interface{}{"all": true})
	require.NoError(t, err)
	assert.NotNil(t, containers)
}

// TestIntegrationDockerRoundTrip exercises the full lifecycle against a live
// daemon: pull -> create -> start -> wait -> logs -> stop -> remove container ->
// remove image. Every created resource is cleaned up via t.Cleanup so the test
// leaves the host as it found it.
func TestIntegrationDockerRoundTrip(t *testing.T) {
	docker := newIntegrationDocker(t)

	// Pull and the lifecycle calls each get their own bounded context, but the
	// overall test budget is generous to allow a real registry pull.
	pullCtx, cancelPull := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelPull()

	reader, err := docker.Pull(pullCtx, integrationImage, PullOptions{})
	require.NoError(t, err, "pull of %s should succeed", integrationImage)
	drain(t, reader)

	// Confirm the image now exists on the host.
	pulled, err := docker.GetImages(context.Background(), integrationImage)
	require.NoError(t, err)
	require.NotEmpty(t, pulled, "pulled image should be listed by GetImages")

	// Remove the image at the very end (after the container that references it
	// is gone). force=true so the removal is robust even if a tag lingers.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Best-effort: a shared host may already have the image in use, so we do
		// not fail the test on cleanup errors.
		_ = docker.RemoveImagesByName(ctx, integrationImage, map[string]interface{}{"force": true})
	})

	containerName := "reagent-integration-" + sanitize(t.Name())

	createCtx, cancelCreate := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCreate()

	id, err := docker.CreateContainer(
		createCtx,
		dtypes.Config{Image: integrationImage},
		dtypes.HostConfig{},
		network.NetworkingConfig{},
		containerName,
	)
	require.NoError(t, err, "create container should succeed")
	require.NotEmpty(t, id, "create should return a non-empty container id")

	// Ensure the container is removed even if a later assertion fails.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = docker.RemoveContainerByID(ctx, id, map[string]interface{}{"force": true, "removeVolumes": true})
	})

	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStart()
	require.NoError(t, docker.StartContainer(startCtx, id), "start container should succeed")

	// hello-world exits on its own; wait for it to stop and assert exit code 0.
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelWait()
	code, err := docker.WaitForContainerByID(waitCtx, id, dtypes.WaitConditionNotRunning)
	require.NoError(t, err, "wait for container should succeed")
	assert.Equal(t, int64(0), code, "hello-world should exit with code 0")

	// Logs should contain the well-known hello-world banner.
	logsCtx, cancelLogs := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLogs()
	logsReader, err := docker.Logs(logsCtx, containerName, map[string]interface{}{
		"stdout": true,
		"stderr": true,
		"follow": false,
	})
	require.NoError(t, err, "fetching logs should succeed")
	require.NotNil(t, logsReader)
	defer logsReader.Close()
	logBytes, err := io.ReadAll(logsReader)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(string(logBytes)), "hello",
		"hello-world logs should mention 'hello'")

	// Explicitly stop (no-op for an already-exited container, but it must not
	// error) and remove the container; the cleanup above is a safety net.
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStop()
	require.NoError(t, docker.StopContainerByID(stopCtx, id, 5*time.Second),
		"stopping an exited container should not error")

	rmCtx, cancelRm := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelRm()
	require.NoError(t, docker.RemoveContainerByID(rmCtx, id, map[string]interface{}{"force": true}),
		"removing the container should succeed")

	// The container should be gone from GetContainer now.
	_, err = docker.GetContainer(context.Background(), containerName)
	assert.Error(t, err, "container should no longer be found after removal")
}

// sanitize turns a test name into a docker-safe container name fragment.
func sanitize(name string) string {
	r := strings.NewReplacer("/", "-", " ", "-", ":", "-")
	return strings.ToLower(r.Replace(name))
}
