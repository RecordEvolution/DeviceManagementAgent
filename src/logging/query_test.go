package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/testutil/builders"
	"reagent/testutil/mocks"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stampedLines renders what Docker returns with Timestamps enabled: an
// RFC3339Nano prefix, a space, then the container's own line.
func stampedLines(count int) string {
	lines := make([]string, 0, count)
	for i := 0; i < count; i++ {
		lines = append(lines, fmt.Sprintf("2026-08-04T09:%02d:00.000000000Z line-%d", i, i))
	}
	return strings.Join(lines, "\n") + "\n"
}

func reader(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}

// alwaysReturn answers every Logs call with a FRESH reader.
//
// A single io.ReadCloser cannot serve two calls — the first drains it — and
// QueryLogs deliberately makes two: the window itself, then an unbounded probe
// for the retention floor. Handing back one instance silently yields an empty
// second read and a zero OldestAvailable.
func alwaysReturn(cont *mocks.Container, containerName string, body string) {
	cont.EXPECT().Logs(mock.Anything, containerName, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ common.Dict) (io.ReadCloser, error) {
			return reader(body), nil
		})
}

func TestQueryLogsTailAndOffset(t *testing.T) {
	t.Run("returns the newest tail and reports the trim", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)
		alwaysReturn(cont, "prod_1_logapp", stampedLines(10))

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			Tail:          3,
		})

		require.NoError(t, err)
		require.Len(t, result.Lines, 3)
		assert.Contains(t, result.Lines[0], "line-7")
		assert.Contains(t, result.Lines[2], "line-9")
		assert.True(t, result.Truncated)
		assert.Equal(t, "docker", result.Source)
	})

	// Docker has no offset, so paging back means over-reading and discarding the
	// newest lines here. Ten lines, drop the newest two, then keep three: 5,6,7.
	t.Run("offset pages further back", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)
		alwaysReturn(cont, "prod_1_logapp", stampedLines(10))

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			Tail:          3,
			Offset:        2,
		})

		require.NoError(t, err)
		require.Len(t, result.Lines, 3)
		assert.Contains(t, result.Lines[0], "line-5")
		assert.Contains(t, result.Lines[2], "line-7")
	})

	t.Run("an offset past the whole log yields nothing rather than the newest lines", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)
		alwaysReturn(cont, "prod_1_logapp", stampedLines(5))

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			Tail:          3,
			Offset:        50,
		})

		require.NoError(t, err)
		assert.Empty(t, result.Lines)
	})

	// The read itself is bounded, not just the answer: asking to page back a
	// million lines must not pull a million lines off the daemon.
	t.Run("bounds the read, not only the result", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)

		// Capture the FIRST call only — the second is the unbounded retention
		// probe, which legitimately asks for everything.
		var seen common.Dict
		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			RunAndReturn(func(_ context.Context, _ string, options common.Dict) (io.ReadCloser, error) {
				if seen == nil {
					seen = options
				}
				return reader(""), nil
			})

		_, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			Tail:          200,
			Offset:        1_000_000,
		})

		require.NoError(t, err)
		assert.Equal(t, fmt.Sprint(MaxQueryLines), seen["tail"])
	})
}

func TestQueryLogsWindow(t *testing.T) {
	t.Run("passes the window through as absolute bounds and asks for timestamps", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)

		// Only the FIRST call is the window; the second is the unbounded
		// retention probe and deliberately carries no bounds.
		var seen common.Dict
		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			RunAndReturn(func(_ context.Context, _ string, options common.Dict) (io.ReadCloser, error) {
				if seen == nil {
					seen = options
				}
				return reader(stampedLines(2)), nil
			})

		_, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			Since:         time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC),
			Until:         time.Date(2026, 8, 4, 9, 15, 0, 0, time.UTC),
		})

		require.NoError(t, err)
		assert.Equal(t, "2026-08-04T09:00:00Z", seen["since"])
		assert.Equal(t, "2026-08-04T09:15:00Z", seen["until"])
		// Without timestamps the caller cannot tell an empty window from a
		// rotated-away one, which is the whole point of the query.
		assert.Equal(t, true, seen["timestamps"])
	})

	// The retention floor comes from a second, unbounded read whose reader is
	// closed after one line — that is what separates "the device was quiet" from
	// "your window is older than what survived rotation".
	t.Run("reports the oldest line the device can still serve", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)
		alwaysReturn(cont, "prod_1_logapp", stampedLines(4))

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{ContainerName: "prod_1_logapp"})

		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), result.OldestAvailable.UTC())
	})
}

func TestQueryLogsFallback(t *testing.T) {
	// A compose project has no container under the plain name, and neither does
	// a removed one. Docker answers ContainerNotFound, compose has no matching
	// project either, and the agent's own history answers instead — flagged as
	// "history" so the caller knows the window was NOT applied.
	t.Run("falls back to stored history and says so", func(t *testing.T) {
		lm, cont, _, db := newTestManager(t)
		addApp(t, db, 1, "logapp", common.PROD)
		require.NoError(t, db.UpsertLogHistory("logapp", 1, common.PROD, []string{"stored-1", "stored-2"}))

		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			Return(nil, errdefs.ContainerNotFound(errors.New("No such container")))
		cont.EXPECT().ListContainers(mock.Anything, mock.Anything).Return(nil, nil)

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{ContainerName: "prod_1_logapp"})

		require.NoError(t, err)
		assert.Equal(t, "history", result.Source)
		assert.Equal(t, []string{"stored-1", "stored-2"}, result.Lines)
		// No retention floor is claimed for a store that has no timestamps.
		assert.True(t, result.OldestAvailable.IsZero())
	})

	// The project's compose file comes from the label Docker keeps on its
	// containers. `docker compose ls` used to find it: a CLI spawn of its own,
	// ahead of the `compose logs` spawn — on a small device, seconds of a
	// console's first load.
	t.Run("reads a compose project through its label, without compose ls", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)

		dir := t.TempDir()
		callsFile := filepath.Join(dir, "calls.log")
		script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\necho 'web-1  | hello'\n", callsFile)
		binPath := filepath.Join(dir, "fake-docker")
		require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			Return(nil, errdefs.ContainerNotFound(errors.New("No such container")))
		cont.EXPECT().ListContainers(mock.Anything, mock.MatchedBy(func(options common.Dict) bool {
			args, ok := options["filters"].(filters.Args)
			return ok && args.ExactMatch("label", "com.docker.compose.project=prod_1_logapp_compose")
		})).Return([]container.ContainerResult{{
			Labels: map[string]string{container.ComposeConfigFilesLabel: "/apps/logapp/docker-compose.json"},
		}}, nil)
		cont.EXPECT().Compose().Return(container.NewComposeWithBinary(builders.DefaultTestConfig(), binPath))

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{ContainerName: "prod_1_logapp", Tail: 10})

		require.NoError(t, err)
		assert.Equal(t, "compose", result.Source)
		assert.Equal(t, []string{"web-1  | hello"}, result.Lines)

		calls, err := os.ReadFile(callsFile)
		require.NoError(t, err)
		assert.Equal(t, "compose -f /apps/logapp/docker-compose.json logs --no-color --tail 10 --timestamps\n", string(calls),
			"exactly one CLI spawn, the read itself")
	})

	t.Run("surfaces a real docker failure instead of quietly serving history", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)
		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			Return(nil, errors.New("permission denied on the docker socket"))

		_, err := lm.QueryLogs(context.Background(), LogQueryRequest{ContainerName: "prod_1_logapp"})

		assert.Error(t, err)
	})

	t.Run("refuses an empty container name", func(t *testing.T) {
		lm, _, _, _ := newTestManager(t)

		_, err := lm.QueryLogs(context.Background(), LogQueryRequest{ContainerName: "  "})

		assert.Error(t, err)
	})
}

func TestParseLeadingTimestamp(t *testing.T) {
	t.Run("reads Docker's RFC3339Nano prefix", func(t *testing.T) {
		parsed := parseLeadingTimestamp("2026-08-04T09:12:04.123456789Z modbus dial failed")

		assert.Equal(t, time.Date(2026, 8, 4, 9, 12, 4, 123456789, time.UTC), parsed.UTC())
	})

	// Docker prefixes an 8-byte stream header on non-TTY containers, which can
	// reach the scanner as leading control bytes.
	t.Run("tolerates a leading stream header", func(t *testing.T) {
		parsed := parseLeadingTimestamp("\x01\x00\x00\x00\x00\x00\x00\x2a2026-08-04T09:12:04Z hello")

		assert.False(t, parsed.IsZero())
	})

	t.Run("returns the zero time for an undated line", func(t *testing.T) {
		assert.True(t, parseLeadingTimestamp("no timestamp here").IsZero())
		assert.True(t, parseLeadingTimestamp("").IsZero())
	})
}

func TestQueryLogsTimestampToggle(t *testing.T) {
	// The console needs them off; a diagnosis needs them on. Either way the
	// retention floor still comes from the separate probe.
	t.Run("timestamps can be turned off for the window", func(t *testing.T) {
		lm, cont, _, _ := newTestManager(t)

		var seen common.Dict
		cont.EXPECT().Logs(mock.Anything, "prod_1_logapp", mock.Anything).
			RunAndReturn(func(_ context.Context, _ string, options common.Dict) (io.ReadCloser, error) {
				if seen == nil {
					seen = options
				}
				return reader(stampedLines(3)), nil
			})

		result, err := lm.QueryLogs(context.Background(), LogQueryRequest{
			ContainerName: "prod_1_logapp",
			NoTimestamps:  true,
		})

		require.NoError(t, err)
		assert.NotContains(t, seen, "timestamps")
		// The probe is independent, so the floor survives the toggle.
		assert.False(t, result.OldestAvailable.IsZero())
	})
}
