package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAgentLog writes zerolog-shaped JSON lines, the format SetupLogger
// produces: one object per line with time, level and message.
func writeAgentLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

func entry(stamp string, level string, message string) string {
	return fmt.Sprintf(`{"level":"%s","time":"%s","caller":"agent.go:12","message":"%s"}`, level, stamp, message)
}

func TestQueryAgentLog(t *testing.T) {
	t.Run("keeps info and worse by default", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path,
			entry("2026-08-04T09:00:00Z", "debug", "polling"),
			entry("2026-08-04T09:01:00Z", "info", "pulling image"),
			entry("2026-08-04T09:02:00Z", "error", "failed to pull image"),
		)

		result, err := QueryAgentLog(AgentLogQuery{Path: path})

		require.NoError(t, err)
		require.Len(t, result.Lines, 2)
		assert.Contains(t, result.Lines[0], "pulling image")
		assert.Contains(t, result.Lines[1], "failed to pull image")
		assert.NotContains(t, strings.Join(result.Lines, "\n"), "polling")
	})

	t.Run("filters to a severity floor", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path,
			entry("2026-08-04T09:01:00Z", "info", "pulling image"),
			entry("2026-08-04T09:02:00Z", "warn", "retrying"),
			entry("2026-08-04T09:03:00Z", "error", "gave up"),
		)

		result, err := QueryAgentLog(AgentLogQuery{Path: path, MinLevel: "warn"})

		require.NoError(t, err)
		require.Len(t, result.Lines, 2)
		assert.Contains(t, result.Lines[0], "retrying")
		assert.Contains(t, result.Lines[1], "gave up")
	})

	t.Run("filters to a time window", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path,
			entry("2026-08-04T08:00:00Z", "info", "too early"),
			entry("2026-08-04T09:05:00Z", "info", "in window"),
			entry("2026-08-04T10:00:00Z", "info", "too late"),
		)

		result, err := QueryAgentLog(AgentLogQuery{
			Path:  path,
			Since: time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC),
			Until: time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC),
		})

		require.NoError(t, err)
		require.Len(t, result.Lines, 1)
		assert.Contains(t, result.Lines[0], "in window")
	})

	// The newest lines are the ones a diagnosis needs, so the ring drops from
	// the front — the opposite of the head-first truncation a naive cap gives.
	t.Run("keeps the newest lines when the tail overflows", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")

		lines := make([]string, 0, 10)
		for i := 0; i < 10; i++ {
			lines = append(lines, entry(fmt.Sprintf("2026-08-04T09:0%d:00Z", i), "info", fmt.Sprintf("line-%d", i)))
		}
		writeAgentLog(t, path, lines...)

		result, err := QueryAgentLog(AgentLogQuery{Path: path, Tail: 3})

		require.NoError(t, err)
		require.Len(t, result.Lines, 3)
		assert.Contains(t, result.Lines[0], "line-7")
		assert.Contains(t, result.Lines[2], "line-9")
		assert.True(t, result.Truncated)
	})

	// A `since` that predates the last rotation must still find its lines, or a
	// windowed question silently loses everything older than the current file.
	t.Run("reads rotated backups oldest first", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")

		writeAgentLog(t, filepath.Join(dir, "reagent-2026-08-03T00-00-00.000.log"),
			entry("2026-08-03T10:00:00Z", "info", "from the older backup"))
		writeAgentLog(t, filepath.Join(dir, "reagent-2026-08-04T00-00-00.000.log"),
			entry("2026-08-04T01:00:00Z", "info", "from the newer backup"))
		writeAgentLog(t, path,
			entry("2026-08-04T09:00:00Z", "info", "from the active file"))

		result, err := QueryAgentLog(AgentLogQuery{Path: path})

		require.NoError(t, err)
		require.Len(t, result.Lines, 3)
		assert.Contains(t, result.Lines[0], "from the older backup")
		assert.Contains(t, result.Lines[1], "from the newer backup")
		assert.Contains(t, result.Lines[2], "from the active file")
		assert.Equal(t, 3, result.FilesScanned)
		assert.Equal(t, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC), result.OldestAvailable.UTC())
	})

	// A panic stack or any raw write lands without JSON around it. Those are
	// exactly the lines worth keeping, so they inherit the preceding entry's
	// time and level instead of being dropped for lacking a date.
	t.Run("keeps non-JSON lines with the entry they follow", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path,
			entry("2026-08-04T09:05:00Z", "error", "panic recovered"),
			"goroutine 1 [running]:",
			"reagent/apps.(*AppManager).Run(...)",
		)

		result, err := QueryAgentLog(AgentLogQuery{
			Path:  path,
			Since: time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC),
		})

		require.NoError(t, err)
		require.Len(t, result.Lines, 3)
		assert.Contains(t, result.Lines[1], "goroutine 1 [running]:")
		assert.Contains(t, result.Lines[2], "AppManager")
	})

	t.Run("renders a compact line, dropping caller noise", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path,
			`{"level":"error","time":"2026-08-04T09:05:00Z","caller":"pull.go:88","error":"unauthorized","message":"failed to pull image"}`)

		result, err := QueryAgentLog(AgentLogQuery{Path: path, MinLevel: "error"})

		require.NoError(t, err)
		require.Len(t, result.Lines, 1)
		assert.Equal(t, "2026-08-04T09:05:00Z ERROR failed to pull image: unauthorized", result.Lines[0])
		assert.NotContains(t, result.Lines[0], "pull.go")
	})

	t.Run("reports an empty window as empty, with a retention floor", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reagent.log")
		writeAgentLog(t, path, entry("2026-08-04T09:00:00Z", "info", "something"))

		result, err := QueryAgentLog(AgentLogQuery{
			Path:  path,
			Since: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		})

		require.NoError(t, err)
		assert.Empty(t, result.Lines)
		// The floor is what lets a caller say "the window is covered and the
		// device was quiet" rather than "this may have rotated away".
		assert.Equal(t, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), result.OldestAvailable.UTC())
	})

	t.Run("fails when there is no log at all", func(t *testing.T) {
		_, err := QueryAgentLog(AgentLogQuery{Path: filepath.Join(t.TempDir(), "reagent.log")})

		assert.Error(t, err)
	})

	t.Run("refuses an unconfigured path", func(t *testing.T) {
		_, err := QueryAgentLog(AgentLogQuery{Path: "  "})

		assert.Error(t, err)
	})
}

func TestTrimToBytes(t *testing.T) {
	t.Run("keeps everything under the limit", func(t *testing.T) {
		lines, trimmed := trimToBytes([]string{"aaa", "bbb"}, 100)

		assert.Equal(t, []string{"aaa", "bbb"}, lines)
		assert.False(t, trimmed)
	})

	t.Run("drops the oldest lines until it fits", func(t *testing.T) {
		lines, trimmed := trimToBytes([]string{"aaa", "bbb", "ccc"}, 8)

		assert.Equal(t, []string{"bbb", "ccc"}, lines)
		assert.True(t, trimmed)
	})
}
