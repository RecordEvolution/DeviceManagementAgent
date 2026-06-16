package persistence

import (
	"reagent/common"
	"reagent/messenger"
	"reagent/system"
	"reagent/testutil/builders"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readDeviceState returns the single current (interface_type, device_status)
// row that the init script seeds and that updateDeviceState mutates in place.
func readDeviceState(t *testing.T, db *AppStateDatabase) (iface string, status string) {
	t.Helper()

	rows, err := db.db.Query(QuerySelectAllDeviceState)
	require.NoError(t, err)
	defer rows.Close()

	require.True(t, rows.Next(), "expected the seeded DeviceStates row to exist")
	require.NoError(t, rows.Scan(&iface, &status))
	require.NoError(t, rows.Err())
	return iface, status
}

// The init script seeds DeviceStates with exactly one row: ('NONE', 'DISCONNECTED').
func TestSeededDeviceState(t *testing.T) {
	db := newTestDB(t)

	iface, status := readDeviceState(t, db)
	assert.Equal(t, "NONE", iface)
	assert.Equal(t, "DISCONNECTED", status)
}

func TestUpdateDeviceStatus(t *testing.T) {
	t.Run("changes the status and leaves the interface untouched", func(t *testing.T) {
		db := newTestDB(t)

		// Seeded status is DISCONNECTED; flipping to CONNECTED is a real change.
		require.NoError(t, db.UpdateDeviceStatus(messenger.CONNECTED))

		iface, status := readDeviceState(t, db)
		assert.Equal(t, string(messenger.CONNECTED), status)
		// An empty interface arg falls back to the current interface_type.
		assert.Equal(t, "NONE", iface)
	})

	t.Run("errors when the status is already the requested one", func(t *testing.T) {
		db := newTestDB(t)

		// Seeded status is already DISCONNECTED.
		err := db.UpdateDeviceStatus(messenger.DISCONNECTED)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the device status is already DISCONNECTED")

		// State is unchanged on the no-op error path.
		iface, status := readDeviceState(t, db)
		assert.Equal(t, "NONE", iface)
		assert.Equal(t, "DISCONNECTED", status)
	})
}

func TestUpdateNetworkInterface(t *testing.T) {
	t.Run("changes the interface and leaves the status untouched", func(t *testing.T) {
		db := newTestDB(t)

		// Seeded interface is NONE; switching to WLAN is a real change.
		require.NoError(t, db.UpdateNetworkInterface(system.WLAN))

		iface, status := readDeviceState(t, db)
		assert.Equal(t, string(system.WLAN), iface)
		// An empty status arg falls back to the current device_status.
		assert.Equal(t, "DISCONNECTED", status)
	})

	t.Run("errors when the interface is already the requested one", func(t *testing.T) {
		db := newTestDB(t)

		// Seeded interface is already NONE.
		err := db.UpdateNetworkInterface(system.NONE)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the current interface is already NONE")

		iface, status := readDeviceState(t, db)
		assert.Equal(t, "NONE", iface)
		assert.Equal(t, "DISCONNECTED", status)
	})

	t.Run("records the previous state in DeviceStateHistory", func(t *testing.T) {
		db := newTestDB(t)

		require.NoError(t, db.UpdateNetworkInterface(system.ETHERNET))

		// The pre-update (interface, status) pair is appended to the history table.
		var iface, status string
		row := db.db.QueryRow(`SELECT interface_type, device_status FROM DeviceStateHistory ORDER BY id DESC LIMIT 1`)
		require.NoError(t, row.Scan(&iface, &status))
		assert.Equal(t, "NONE", iface)
		assert.Equal(t, "DISCONNECTED", status)

		// And the live row now reflects the new interface.
		liveIface, liveStatus := readDeviceState(t, db)
		assert.Equal(t, string(system.ETHERNET), liveIface)
		assert.Equal(t, "DISCONNECTED", liveStatus)
	})
}

// updateDeviceState is the shared implementation. Driving it through both public
// wrappers in sequence exercises the fallback branches: each call keeps the
// dimension it does not own.
func TestUpdateDeviceStateSequential(t *testing.T) {
	db := newTestDB(t)

	require.NoError(t, db.UpdateNetworkInterface(system.WLAN))
	require.NoError(t, db.UpdateDeviceStatus(messenger.CONNECTED))

	iface, status := readDeviceState(t, db)
	assert.Equal(t, string(system.WLAN), iface)
	assert.Equal(t, string(messenger.CONNECTED), status)
}

// QueueTask hands the closure to the consumer goroutine started in Init()
// ("for task := range sqlite.updateQueue"), so queued work actually runs.
func TestQueueTaskRunsQueuedWork(t *testing.T) {
	db := newTestDB(t)

	var ran atomic.Bool
	db.QueueTask(func() { ran.Store(true) })

	require.Eventually(t, ran.Load, time.Second, 5*time.Millisecond,
		"queued task should have been executed by the consumer goroutine")
}

func TestQueueTaskPersistsThroughTheQueue(t *testing.T) {
	db := newTestDB(t)

	app := builders.BuildApp("queued-app", common.PRESENT, common.PROD)
	app.AppKey = 4242

	// The closure performs a real DB write; we only observe its committed effect.
	db.QueueTask(func() {
		_, _ = db.UpsertAppState(app, common.PRESENT)
	})

	require.Eventually(t, func() bool {
		got, err := db.GetAppState(4242, common.PROD)
		return err == nil && got != nil
	}, time.Second, 5*time.Millisecond, "queued upsert should become visible")

	got, err := db.GetAppState(4242, common.PROD)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "queued-app", got.AppName)
	assert.Equal(t, common.PRESENT, got.CurrentState)
}

// Every queued task is delivered to the single consumer goroutine and runs
// exactly once — none are dropped. (Arrival order is intentionally not asserted:
// QueueTask fans each enqueue out via safe.Go, so sends race to the unbuffered
// channel and the order they reach the consumer is nondeterministic.)
func TestQueueTaskRunsEveryQueuedTask(t *testing.T) {
	db := newTestDB(t)

	const n = 50
	var count atomic.Int64
	for i := 0; i < n; i++ {
		db.QueueTask(func() { count.Add(1) })
	}

	require.Eventually(t, func() bool {
		return count.Load() == int64(n)
	}, 2*time.Second, 5*time.Millisecond, "all queued tasks should run exactly once")

	// And no extra invocations sneak in afterwards.
	assert.Equal(t, int64(n), count.Load())
}
