package persistence

import (
	"path/filepath"
	"reagent/common"
	"reagent/testutil/builders"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDB spins up a fresh sqlite database backed by a temp file (NOT
// ":memory:", which NewSQLiteDb's DSN mangling would break), runs the init
// scripts, and registers cleanup.
func newTestDB(t *testing.T) *AppStateDatabase {
	t.Helper()

	cfg := builders.DefaultTestConfig()
	cfg.CommandLineArguments.DatabaseFileName = filepath.Join(t.TempDir(), "test.db")

	db, err := NewSQLiteDb(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Init())

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// newRequestedPayload returns a TransitionPayload that satisfies the
// RequestedAppStates NOT NULL / CHECK constraints (valid enum states).
func newRequestedPayload(t *testing.T, name string, appKey uint64, stage common.Stage) common.TransitionPayload {
	t.Helper()

	p := builders.BuildTransitionPayload(name, common.RUNNING, stage)
	p.AppKey = appKey
	p.CurrentState = common.PRESENT
	p.RequestorAccountKey = 42
	return p
}

func TestUpsertAndGetAppState(t *testing.T) {
	db := newTestDB(t)

	app := builders.BuildApp("my-app", common.PRESENT, common.PROD)
	app.AppKey = 100
	app.ReleaseKey = 7

	// First upsert performs an INSERT (no existing row) and returns a timestamp.
	ts, err := db.UpsertAppState(app, common.PRESENT)
	require.NoError(t, err)
	assert.NotEmpty(t, string(ts))

	got, err := db.GetAppState(100, common.PROD)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "my-app", got.AppName)
	assert.Equal(t, uint64(100), got.AppKey)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, uint64(7), got.ReleaseKey)
	assert.Equal(t, common.PROD, got.Stage)
	assert.Equal(t, common.PRESENT, got.CurrentState)
}

func TestUpsertAppStateNoChangeIsNoop(t *testing.T) {
	db := newTestDB(t)

	app := builders.BuildApp("noop-app", common.PRESENT, common.PROD)
	app.AppKey = 200

	_, err := db.UpsertAppState(app, common.PRESENT)
	require.NoError(t, err)

	// Same state/version/release => silently does nothing, empty timestamp.
	ts, err := db.UpsertAppState(app, common.PRESENT)
	require.NoError(t, err)
	assert.Empty(t, string(ts))
}

func TestUpsertAppStateTransitionUpdatesCurrentState(t *testing.T) {
	db := newTestDB(t)

	app := builders.BuildApp("transition-app", common.PRESENT, common.PROD)
	app.AppKey = 300

	_, err := db.UpsertAppState(app, common.PRESENT)
	require.NoError(t, err)

	// Transition to RUNNING => updates the current state and returns a (history) timestamp.
	ts, err := db.UpsertAppState(app, common.RUNNING)
	require.NoError(t, err)
	assert.NotEmpty(t, string(ts))

	got, err := db.GetAppState(300, common.PROD)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, common.RUNNING, got.CurrentState)
}

func TestGetAppStateNotFound(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetAppState(9999, common.PROD)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetAppStates(t *testing.T) {
	db := newTestDB(t)

	app1 := builders.BuildApp("app-one", common.PRESENT, common.PROD)
	app1.AppKey = 1
	app2 := builders.BuildApp("app-two", common.RUNNING, common.DEV)
	app2.AppKey = 2

	_, err := db.UpsertAppState(app1, common.PRESENT)
	require.NoError(t, err)
	_, err = db.UpsertAppState(app2, common.RUNNING)
	require.NoError(t, err)

	apps, err := db.GetAppStates()
	require.NoError(t, err)
	require.Len(t, apps, 2)

	byKey := map[uint64]*common.App{}
	for _, a := range apps {
		byKey[a.AppKey] = a
	}
	require.Contains(t, byKey, uint64(1))
	require.Contains(t, byKey, uint64(2))
	assert.Equal(t, "app-one", byKey[1].AppName)
	assert.Equal(t, common.PRESENT, byKey[1].CurrentState)
	assert.Equal(t, "app-two", byKey[2].AppName)
	assert.Equal(t, common.RUNNING, byKey[2].CurrentState)
}

func TestGetAppStatesEmpty(t *testing.T) {
	db := newTestDB(t)

	apps, err := db.GetAppStates()
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestDeleteAppState(t *testing.T) {
	db := newTestDB(t)

	app := builders.BuildApp("doomed", common.PRESENT, common.PROD)
	app.AppKey = 400

	_, err := db.UpsertAppState(app, common.PRESENT)
	require.NoError(t, err)

	require.NoError(t, db.DeleteAppState(400, common.PROD))

	got, err := db.GetAppState(400, common.PROD)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Deleting a non-existent row is not an error.
	require.NoError(t, db.DeleteAppState(400, common.PROD))
}

func TestUpsertAndGetRequestedState(t *testing.T) {
	db := newTestDB(t)

	payload := newRequestedPayload(t, "req-app", 500, common.PROD)
	payload.EnvironmentVariables = map[string]any{"FOO": "bar"}
	payload.Ports = []any{"8080:80"}
	payload.NewestVersion = "2.0.0"
	payload.PresentVersion = "1.0.0"

	require.NoError(t, db.UpsertRequestedStateChange(payload))

	got, err := db.GetRequestedState(500, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, "req-app", got.AppName)
	assert.Equal(t, uint64(500), got.AppKey)
	assert.Equal(t, common.PROD, got.Stage)
	assert.Equal(t, common.RUNNING, got.RequestedState)
	assert.Equal(t, common.PRESENT, got.CurrentState)
	assert.Equal(t, uint64(42), got.RequestorAccountKey)
	assert.Equal(t, "2.0.0", got.NewestVersion)
	assert.Equal(t, "1.0.0", got.PresentVersion)
	assert.Equal(t, "bar", got.EnvironmentVariables["FOO"])
	require.Len(t, got.Ports, 1)
	assert.Equal(t, "8080:80", got.Ports[0])
	// DockerCompose was serialized from the builder default.
	assert.Equal(t, "3", got.DockerCompose["version"])
}

func TestUpsertRequestedStateDeviceOwnerAccountKey(t *testing.T) {
	db := newTestDB(t)

	payload := newRequestedPayload(t, "owner-app", 520, common.PROD)
	payload.DeviceOwnerAccountKey = 711
	require.NoError(t, db.UpsertRequestedStateChange(payload))

	got, err := db.GetRequestedState(520, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, uint64(711), got.DeviceOwnerAccountKey)

	// A payload without the owner key (locally built, e.g. mid-transition state
	// writes) must not clobber the stored value with 0.
	payload.DeviceOwnerAccountKey = 0
	require.NoError(t, db.UpsertRequestedStateChange(payload))

	got, err = db.GetRequestedState(520, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, uint64(711), got.DeviceOwnerAccountKey)

	// A cloud payload carrying a new owner (device/project transferred) wins.
	payload.DeviceOwnerAccountKey = 999
	require.NoError(t, db.UpsertRequestedStateChange(payload))

	got, err = db.GetRequestedState(520, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, uint64(999), got.DeviceOwnerAccountKey)
}

func TestUpsertRequestedStateChangeOverwrites(t *testing.T) {
	db := newTestDB(t)

	payload := newRequestedPayload(t, "upd-app", 510, common.PROD)
	require.NoError(t, db.UpsertRequestedStateChange(payload))

	// Conflicting (app_name, app_key, stage) updates in place.
	payload.RequestedState = common.STOPPING
	payload.RequestorAccountKey = 99
	require.NoError(t, db.UpsertRequestedStateChange(payload))

	got, err := db.GetRequestedState(510, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, common.STOPPING, got.RequestedState)
	assert.Equal(t, uint64(99), got.RequestorAccountKey)

	// Still only one logical record (no duplicate rows for the conflict key).
	all, err := db.GetRequestedStates()
	require.NoError(t, err)
	count := 0
	for _, p := range all {
		if p.AppKey == 510 {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestGetRequestedStateNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetRequestedState(123456, common.PROD)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no requested state found")
}

func TestGetRequestedStates(t *testing.T) {
	db := newTestDB(t)

	require.NoError(t, db.UpsertRequestedStateChange(newRequestedPayload(t, "a", 1, common.PROD)))
	require.NoError(t, db.UpsertRequestedStateChange(newRequestedPayload(t, "b", 2, common.DEV)))

	all, err := db.GetRequestedStates()
	require.NoError(t, err)
	require.Len(t, all, 2)

	byKey := map[uint64]common.TransitionPayload{}
	for _, p := range all {
		byKey[p.AppKey] = p
	}
	require.Contains(t, byKey, uint64(1))
	require.Contains(t, byKey, uint64(2))
	assert.Equal(t, "a", byKey[1].AppName)
	assert.Equal(t, common.PROD, byKey[1].Stage)
	assert.Equal(t, "b", byKey[2].AppName)
	assert.Equal(t, common.DEV, byKey[2].Stage)
}

func TestGetRequestedStatesEmpty(t *testing.T) {
	db := newTestDB(t)

	all, err := db.GetRequestedStates()
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestBulkUpsertRequestedStateChanges(t *testing.T) {
	db := newTestDB(t)

	payloads := []common.TransitionPayload{
		newRequestedPayload(t, "bulk-1", 600, common.PROD),
		newRequestedPayload(t, "bulk-2", 601, common.DEV),
		newRequestedPayload(t, "bulk-3", 602, common.PROD),
	}

	require.NoError(t, db.BulkUpsertRequestedStateChanges(payloads))

	all, err := db.GetRequestedStates()
	require.NoError(t, err)
	assert.Len(t, all, 3)

	got, err := db.GetRequestedState(601, common.DEV)
	require.NoError(t, err)
	assert.Equal(t, "bulk-2", got.AppName)
	assert.Equal(t, common.DEV, got.Stage)
}

func TestBulkUpsertRequestedStateChangesEmpty(t *testing.T) {
	db := newTestDB(t)

	// An empty batch commits cleanly without touching anything.
	require.NoError(t, db.BulkUpsertRequestedStateChanges(nil))

	all, err := db.GetRequestedStates()
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestDeleteRequestedState(t *testing.T) {
	db := newTestDB(t)

	require.NoError(t, db.UpsertRequestedStateChange(newRequestedPayload(t, "del-req", 700, common.PROD)))

	require.NoError(t, db.DeleteRequestedState(700, common.PROD))

	_, err := db.GetRequestedState(700, common.PROD)
	require.Error(t, err)

	// Deleting a non-existent requested state is not an error.
	require.NoError(t, db.DeleteRequestedState(700, common.PROD))
}

func TestUpsertAndGetLogHistory(t *testing.T) {
	db := newTestDB(t)

	logs := []string{"line one", "line two", "line three"}
	require.NoError(t, db.UpsertLogHistory("log-app", 800, common.PROD, logs))

	got, err := db.GetAppLogHistory("log-app", 800, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, logs, got)
}

func TestUpsertLogHistoryOverwrites(t *testing.T) {
	db := newTestDB(t)

	require.NoError(t, db.UpsertLogHistory("log-app", 810, common.PROD, []string{"old"}))
	require.NoError(t, db.UpsertLogHistory("log-app", 810, common.PROD, []string{"new-1", "new-2"}))

	got, err := db.GetAppLogHistory("log-app", 810, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, []string{"new-1", "new-2"}, got)
}

func TestGetAppLogHistoryNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetAppLogHistory("missing", 9999, common.PROD)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no logs found")
}

func TestClearAllLogHistory(t *testing.T) {
	db := newTestDB(t)

	require.NoError(t, db.UpsertLogHistory("clear-app", 900, common.PROD, []string{"a", "b"}))

	require.NoError(t, db.ClearAllLogHistory("clear-app", 900, common.PROD))

	got, err := db.GetAppLogHistory("clear-app", 900, common.PROD)
	require.NoError(t, err)
	assert.Empty(t, got)
}
