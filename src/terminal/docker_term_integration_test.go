//go:build integration

// This test drives the real docker-exec backed TerminalManager against a live
// Docker daemon. It is excluded from `just test` and runs only under
// `just test-integration` (-tags integration). See src/TESTING.md.
//
// It SKIPS cleanly (does not fail) whenever the resource is unavailable: no
// Docker daemon reachable, or the throwaway image/container cannot be created.
// That keeps `just test-integration` green in environments without Docker.
package terminal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"reagent/container"
	"reagent/testutil/builders"
	"reagent/testutil/fakes"
	"testing"
	"time"

	dtypes "github.com/docker/docker/api/types/container"
	dnetwork "github.com/docker/docker/api/types/network"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	integrationImage    = "alpine:latest"
	pingTimeout         = 10 * time.Second
	pullTimeout         = 120 * time.Second
	containerOpsTimeout = 60 * time.Second
	terminalReadWindow  = 20 * time.Second
)

// newDockerOrSkip builds a real Docker client and pings the daemon. It skips the
// test (rather than failing) when no daemon is reachable, so the suite stays
// green on machines without Docker.
func newDockerOrSkip(t *testing.T) *container.Docker {
	t.Helper()

	cfg := builders.DefaultTestConfig()

	docker, err := container.NewDocker(cfg)
	if err != nil {
		t.Skipf("skipping: cannot create docker client (no daemon?): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if _, err := docker.Ping(ctx); err != nil {
		t.Skipf("skipping: docker daemon not reachable: %v", err)
	}

	return docker
}

// ensureImageOrSkip makes sure the throwaway image is present locally, pulling
// it if necessary. Skips on pull failure (e.g. no network in this env).
func ensureImageOrSkip(t *testing.T, docker *container.Docker) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), containerOpsTimeout)
	if imgs, err := docker.GetImages(ctx, integrationImage); err == nil && len(imgs) > 0 {
		cancel()
		return
	}
	cancel()

	pullCtx, pullCancel := context.WithTimeout(context.Background(), pullTimeout)
	defer pullCancel()

	reader, err := docker.Pull(pullCtx, integrationImage, container.PullOptions{PullID: uuid.NewString()})
	if err != nil {
		t.Skipf("skipping: cannot pull %s (no network/registry?): %v", integrationImage, err)
	}
	defer reader.Close()

	// Drain the pull stream so the image is fully materialized before use.
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Skipf("skipping: error draining pull stream for %s: %v", integrationImage, err)
	}
}

// startThrowawayContainer creates and starts a long-lived alpine container and
// registers cleanup to force-remove it. Returns the container name.
func startThrowawayContainer(t *testing.T, docker *container.Docker) string {
	t.Helper()

	name := fmt.Sprintf("reagent-term-it-%s", uuid.NewString()[:8])

	cConfig := dtypes.Config{
		Image: integrationImage,
		// Keep the container alive so we can exec a shell into it.
		Cmd: []string{"tail", "-f", "/dev/null"},
		Tty: false,
	}

	createCtx, createCancel := context.WithTimeout(context.Background(), containerOpsTimeout)
	defer createCancel()

	id, err := docker.CreateContainer(createCtx, cConfig, dtypes.HostConfig{}, dnetwork.NetworkingConfig{}, name)
	require.NoError(t, err, "failed to create throwaway container")

	// Register removal immediately so a later failure still cleans up.
	t.Cleanup(func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), containerOpsTimeout)
		defer rmCancel()
		_ = docker.RemoveContainerByName(rmCtx, name, map[string]interface{}{"force": true, "removeVolumes": true})
	})

	startCtx, startCancel := context.WithTimeout(context.Background(), containerOpsTimeout)
	defer startCancel()

	require.NoError(t, docker.StartContainer(startCtx, id), "failed to start throwaway container")

	// Wait until it reports running so exec attaches succeed.
	runCtx, runCancel := context.WithTimeout(context.Background(), containerOpsTimeout)
	defer runCancel()

	runningC, errC := docker.WaitForRunning(runCtx, id, 200*time.Millisecond)
	select {
	case <-runningC:
	case err := <-errC:
		require.NoError(t, err, "throwaway container did not reach running state")
	case <-runCtx.Done():
		t.Fatalf("timed out waiting for throwaway container to run")
	}

	return name
}

// TestTerminalManager_RealExecSession_RoundTrip opens a real shell session in a
// live container via the manager's RequestTerminalSession path (real getShell +
// ExecAttach), writes a command to the hijacked exec connection, and reads the
// echoed marker back from the real PTY, then tears the session down.
//
// It drives the round-trip directly on the hijacked connection (rather than
// through StartTerminalSession's reader goroutine) so the assertion does not
// race the fake messenger's internal call-recording slice.
func TestTerminalManager_RealExecSession_RoundTrip(t *testing.T) {
	docker := newDockerOrSkip(t)
	ensureImageOrSkip(t, docker)

	containerName := startThrowawayContainer(t, docker)

	fakeMsg := fakes.NewMessenger()
	tm := NewTerminalManager(fakeMsg, docker)

	// RequestTerminalSession exercises the real getShell (ExecCommand
	// "cat /etc/shells") and createTerminalSession (ExecAttach) paths and
	// registers the session in ActiveSessions.
	session, err := tm.RequestTerminalSession(containerName)
	require.NoError(t, err, "failed to request real terminal session")
	require.NotNil(t, session)
	require.NotEmpty(t, session.SessionID)
	require.NotEmpty(t, session.DataTopic)
	require.NotNil(t, session.Session, "expected a hijacked exec response")
	require.NotNil(t, session.Session.Conn)
	require.NotNil(t, session.Session.Reader)
	require.NotEmpty(t, session.Session.ExecID, "expected a docker exec ID for the attached shell")

	// Ensure the session is cleaned up (closes the hijacked connection and
	// removes it from ActiveSessions) even if a later assertion fails.
	t.Cleanup(func() {
		_ = tm.StopTerminalSession(session.SessionID)
	})

	// Write a command with a unique marker to the exec'd shell's stdin. A TTY
	// shell echoes the input line and prints the marker on stdout, which the
	// hijacked Reader surfaces.
	marker := "REAGENT_IT_" + uuid.NewString()[:12]
	command := fmt.Sprintf("echo %s\n", marker)

	_ = session.Session.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = session.Session.Conn.Write([]byte(command))
	require.NoError(t, err, "failed to write command to hijacked exec connection")

	markerBytes := []byte(marker)
	require.True(t,
		readUntilMarker(t, session.Session.Reader, session.Session.Conn, markerBytes),
		"expected marker %q in PTY output within %s", marker, terminalReadWindow,
	)

	// Tearing down should remove the session from the manager's active set.
	require.NoError(t, tm.StopTerminalSession(session.SessionID), "failed to stop terminal session")

	_, err = tm.getSession(session.SessionID)
	require.Error(t, err, "expected session to be gone from ActiveSessions after StopTerminalSession")
}

// readUntilMarker reads from the hijacked exec reader until the marker is seen
// or the read window elapses. It uses a read deadline on the connection so a
// quiet PTY cannot block the test forever.
func readUntilMarker(t *testing.T, reader *bufio.Reader, conn interface{ SetReadDeadline(time.Time) error }, marker []byte) bool {
	t.Helper()

	var acc bytes.Buffer
	buf := make([]byte, 4096)
	deadline := time.Now().Add(terminalReadWindow)

	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := reader.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if bytes.Contains(acc.Bytes(), marker) {
				return true
			}
		}
		if err != nil {
			// Timeout on a quiet PTY is expected; keep looping until the
			// overall window elapses. Any other error ends the read loop.
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}
			return bytes.Contains(acc.Bytes(), marker)
		}
	}

	return bytes.Contains(acc.Bytes(), marker)
}
