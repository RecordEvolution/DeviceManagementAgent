package logging

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"
	"reagent/store"
	"reagent/testutil/builders"
	"reagent/testutil/fakes"
	"reagent/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDB builds a real, isolated SQLite-backed persistence DB in a temp dir
// with the schema initialized. Cleanup closes it.
func newTestDB(t *testing.T) persistence.Database {
	t.Helper()

	cfg := builders.DefaultTestConfig()
	cfg.CommandLineArguments.DatabaseFileName = t.TempDir() + "/reagent_test.db"

	db, err := persistence.NewSQLiteDb(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Init())

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestManager wires a LogManager with a real DB + store, a fake messenger,
// and a strict container mock. The container mock has no expectations by
// default; tests that exercise the Container.Logs fallback set them up.
func newTestManager(t *testing.T) (*LogManager, *mocks.Container, *fakes.Messenger, persistence.Database) {
	t.Helper()

	db := newTestDB(t)
	msg := fakes.NewMessenger()
	cont := mocks.NewContainer(t)
	as := store.NewAppStore(db, msg)

	lm := NewLogManager(cont, msg, db, as)
	return &lm, cont, msg, db
}

// addApp inserts an app into the store-backed DB so that getPersistedLogHistory
// finds a non-nil app (and thus consults the LogHistory table) for the given
// container coordinates.
func addApp(t *testing.T, db persistence.Database, appKey uint64, appName string, stage common.Stage) {
	t.Helper()
	app := builders.BuildApp(appName, common.RUNNING, stage)
	app.AppKey = appKey
	_, err := db.UpsertAppState(app, common.RUNNING)
	require.NoError(t, err)
}

func nopReadCloser(s string) io.ReadCloser {
	return io.NopCloser(bytes.NewReader([]byte(s)))
}

func TestBuildTopic(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	serial := msg.GetConfig().ReswarmConfig.SerialNumber
	topic := lm.buildTopic("prod_1_myapp")

	assert.Equal(t, "reswarm.logs."+serial+".prod_1_myapp", topic)
}

func TestPublish(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	err := lm.Publish("prod_1_myapp", "hello world")
	require.NoError(t, err)

	require.Len(t, msg.PublishCalls, 1)
	call := msg.PublishCalls[0]

	serial := msg.GetConfig().ReswarmConfig.SerialNumber
	assert.Equal(t, topics.Topic("reswarm.logs."+serial+".prod_1_myapp"), call.Topic)

	require.Len(t, call.Args, 1)
	dict, ok := call.Args[0].(common.Dict)
	require.True(t, ok)
	assert.Equal(t, "build", dict["type"])
	assert.Equal(t, "hello world", dict["chunk"])
}

func TestClearRemotePublishesClearChunk(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	err := lm.ClearRemote("prod_1_myapp")
	require.NoError(t, err)

	require.Len(t, msg.PublishCalls, 1)
	dict, ok := msg.PublishCalls[0].Args[0].(common.Dict)
	require.True(t, ok)
	assert.Equal(t, "build", dict["type"])
	assert.Equal(t, string(LOGGER_CLEAR), dict["chunk"])
}

func TestPublishProgress(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	err := lm.PublishProgress("prod_1_myapp", "id-7", "Downloading", "50%")
	require.NoError(t, err)

	require.Len(t, msg.PublishCalls, 1)
	call := msg.PublishCalls[0]

	serial := msg.GetConfig().ReswarmConfig.SerialNumber
	assert.Equal(t, topics.Topic("reswarm.logs."+serial+".prod_1_myapp"), call.Topic)

	dict, ok := call.Args[0].(common.Dict)
	require.True(t, ok)
	assert.Equal(t, "id-7", dict["id"])
	assert.Equal(t, "Downloading", dict["status"])
	assert.Equal(t, "50%", dict["progress"])
}

func TestWriteAppendsAgentLogAndPublishes(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	containerName := "prod_1_myapp"

	// Seed an active log process so Write has somewhere to append the AGENT log.
	lp := &LogProccess{
		ContainerName: containerName,
		logHistory:    make([]*LogEntry, 0),
	}
	lm.activeLogs[containerName] = lp

	err := lm.Write(containerName, "agent says hi")
	require.NoError(t, err)

	// Published the build chunk.
	require.Len(t, msg.PublishCalls, 1)
	dict, ok := msg.PublishCalls[0].Args[0].(common.Dict)
	require.True(t, ok)
	assert.Equal(t, "build", dict["type"])
	assert.Equal(t, "agent says hi", dict["chunk"])

	// Appended an AGENT-typed entry to the active log's history.
	require.Len(t, lp.logHistory, 1)
	assert.Equal(t, "agent says hi", lp.logHistory[0].entry)
	assert.Equal(t, AGENT, lp.logHistory[0].logType)
}

func TestWriteWithoutActiveLogStillPublishes(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	err := lm.Write("prod_1_other", "no active log")
	require.NoError(t, err)

	require.Len(t, msg.PublishCalls, 1)
	dict, ok := msg.PublishCalls[0].Args[0].(common.Dict)
	require.True(t, ok)
	assert.Equal(t, "no active log", dict["chunk"])
}

func TestLogEntriesToString(t *testing.T) {
	entries := []*LogEntry{
		{entry: "a", logType: CONTAINER},
		{entry: "b", logType: AGENT},
		{entry: "c", logType: CONTAINER},
	}
	assert.Equal(t, []string{"a", "b", "c"}, logEntriesToString(entries))

	assert.Empty(t, logEntriesToString(nil))
}

func TestAppendLog(t *testing.T) {
	lp := &LogProccess{logHistory: make([]*LogEntry, 0)}
	lp.appendLog(LogEntry{entry: "one", logType: CONTAINER})
	lp.appendLog(LogEntry{entry: "two", logType: AGENT})

	require.Len(t, lp.logHistory, 2)
	assert.Equal(t, "one", lp.logHistory[0].entry)
	assert.Equal(t, "two", lp.logHistory[1].entry)
	assert.Equal(t, AGENT, lp.logHistory[1].logType)
}

func TestGetLogHistory(t *testing.T) {
	t.Run("returns persisted container history without hitting the container", func(t *testing.T) {
		lm, _, _, db := newTestManager(t)

		// prod_1_myapp parses to stage=PROD, appKey=1, name=myapp.
		addApp(t, db, 1, "myapp", common.PROD)
		require.NoError(t, db.UpsertLogHistory("myapp", 1, common.PROD, []string{"line-1", "line-2"}))

		// The strict container mock has no Logs expectation, so if GetLogHistory
		// fell through to the container it would fail the test.
		got, err := lm.GetLogHistory("prod_1_myapp")
		require.NoError(t, err)
		assert.Equal(t, []string{"line-1", "line-2"}, got)
	})

	t.Run("falls back to container logs when no history exists", func(t *testing.T) {
		lm, cont, _, db := newTestManager(t)

		addApp(t, db, 2, "freshapp", common.PROD)
		// No log history persisted -> empty history -> container fallback.

		cont.EXPECT().
			Logs(context.Background(), "prod_2_freshapp", common.Dict{
				"follow": false, "stdout": true, "stderr": true, "tail": "50",
			}).
			Return(nopReadCloser("docker-line-1\ndocker-line-2\n"), nil).
			Once()

		got, err := lm.GetLogHistory("prod_2_freshapp")
		require.NoError(t, err)
		assert.Equal(t, []string{"docker-line-1", "docker-line-2"}, got)
	})

	t.Run("returns empty when container fallback errors", func(t *testing.T) {
		lm, cont, _, db := newTestManager(t)

		addApp(t, db, 3, "errapp", common.PROD)

		cont.EXPECT().
			Logs(context.Background(), "prod_3_errapp", common.Dict{
				"follow": false, "stdout": true, "stderr": true, "tail": "50",
			}).
			Return(nil, assert.AnError).
			Once()

		got, err := lm.GetLogHistory("prod_3_errapp")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestGetPersistedLogHistoryPrefersInMemory(t *testing.T) {
	lm, _, _, _ := newTestManager(t)

	containerName := "prod_5_memapp"
	lp := &LogProccess{
		ContainerName: containerName,
		logHistory: []*LogEntry{
			{entry: "mem-1", logType: CONTAINER},
		},
	}
	lm.activeLogs[containerName] = lp

	// No app in DB and no DB history, but the in-memory active log has entries,
	// so those win and no DB/app lookup error surfaces.
	history, err := lm.getPersistedLogHistory(containerName)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "mem-1", history[0].entry)
}

func TestGetPersistedLogHistoryInvalidContainerName(t *testing.T) {
	lm, _, _, _ := newTestManager(t)

	_, err := lm.getPersistedLogHistory("not-valid")
	require.Error(t, err)
}

func TestClearLogHistoryClearsInMemoryAndDatabase(t *testing.T) {
	lm, _, _, db := newTestManager(t)

	containerName := "prod_9_clearapp"
	addApp(t, db, 9, "clearapp", common.PROD)
	require.NoError(t, db.UpsertLogHistory("clearapp", 9, common.PROD, []string{"keep-me-not"}))

	// Seed in-memory history that should be cleared synchronously.
	lp := &LogProccess{
		ContainerName: containerName,
		logHistory: []*LogEntry{
			{entry: "in-mem", logType: CONTAINER},
		},
	}
	lm.activeLogs[containerName] = lp

	err := lm.ClearLogHistory(containerName)
	require.NoError(t, err)

	// In-memory cleared right away.
	lp.subscriptionStateMutex.Lock()
	memLen := len(lp.logHistory)
	lp.subscriptionStateMutex.Unlock()
	assert.Equal(t, 0, memLen)

	// DB clear happens on a goroutine; poll until the row is emptied.
	require.Eventually(t, func() bool {
		logs, err := db.GetAppLogHistory("clearapp", 9, common.PROD)
		if err != nil {
			return false
		}
		return len(logs) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestGetActiveSubscriptionID(t *testing.T) {
	t.Run("returns stringified id from lookup", func(t *testing.T) {
		lm, _, msg, _ := newTestManager(t)

		msg.SetCallResponse(string(topics.MetaProcLookupSubscription), messenger.Result{
			Arguments: []interface{}{uint64(987654)},
		}, nil)

		id, err := lm.getActiveSubscriptionID("prod_1_myapp")
		require.NoError(t, err)
		assert.Equal(t, "987654", id)

		// Verify the lookup used the right topic and the built log topic as the
		// first argument.
		require.Len(t, msg.CallCalls, 1)
		call := msg.CallCalls[0]
		assert.Equal(t, topics.MetaProcLookupSubscription, call.Topic)
		require.Len(t, call.Args, 2)
		assert.Equal(t, lm.buildTopic("prod_1_myapp"), call.Args[0])
	})

	t.Run("empty arguments yields empty id", func(t *testing.T) {
		lm, _, msg, _ := newTestManager(t)

		msg.SetCallResponse(string(topics.MetaProcLookupSubscription), messenger.Result{
			Arguments: []interface{}{},
		}, nil)

		id, err := lm.getActiveSubscriptionID("prod_1_myapp")
		require.NoError(t, err)
		assert.Equal(t, "", id)
	})

	t.Run("nil id yields empty id", func(t *testing.T) {
		lm, _, msg, _ := newTestManager(t)

		msg.SetCallResponse(string(topics.MetaProcLookupSubscription), messenger.Result{
			Arguments: []interface{}{nil},
		}, nil)

		id, err := lm.getActiveSubscriptionID("prod_1_myapp")
		require.NoError(t, err)
		assert.Equal(t, "", id)
	})

	t.Run("propagates call error", func(t *testing.T) {
		lm, _, msg, _ := newTestManager(t)

		msg.SetCallError(string(topics.MetaProcLookupSubscription), assert.AnError)

		_, err := lm.getActiveSubscriptionID("prod_1_myapp")
		require.Error(t, err)
	})
}

func TestSetPublishStateByContainerName(t *testing.T) {
	lm, _, _, _ := newTestManager(t)

	containerName := "prod_1_myapp"
	lp := &LogProccess{ContainerName: containerName}
	// addLogProcess kicks off goroutines we don't want here; populate the slice
	// directly to exercise just the state mutation.
	lm.logProcessEntries = append(lm.logProcessEntries, lp)

	lm.setPublishStateByContainerName(containerName, "sub-42", true)

	lp.subscriptionStateMutex.Lock()
	publish := lp.Publish
	subID := lp.SubscriptionID
	lp.subscriptionStateMutex.Unlock()

	assert.True(t, publish)
	assert.Equal(t, "sub-42", subID)

	// A different container name must not be touched.
	other := &LogProccess{ContainerName: "prod_2_other"}
	lm.logProcessEntries = append(lm.logProcessEntries, other)
	lm.setPublishStateByContainerName("prod_2_other", "sub-99", false)

	other.subscriptionStateMutex.Lock()
	assert.False(t, other.Publish)
	assert.Equal(t, "sub-99", other.SubscriptionID)
	other.subscriptionStateMutex.Unlock()
}

func TestGetLogTaskBySubscriptionID(t *testing.T) {
	lm, _, _, _ := newTestManager(t)

	match := &LogProccess{ContainerName: "prod_1_myapp", SubscriptionID: "the-id"}
	lm.activeLogs["prod_1_myapp"] = match
	lm.activeLogs["prod_2_other"] = &LogProccess{ContainerName: "prod_2_other", SubscriptionID: "other-id"}

	got := lm.GetLogTaskBySubscriptionID("the-id")
	require.NotNil(t, got)
	assert.Same(t, match, got)

	assert.Nil(t, lm.GetLogTaskBySubscriptionID("does-not-exist"))
}

func TestStreamLogsChannelRegistersProcess(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	containerName := "prod_1_myapp"

	// No active subscription -> Publish stays false.
	msg.SetCallResponse(string(topics.MetaProcLookupSubscription), messenger.Result{
		Arguments: []interface{}{},
	}, nil)

	ch := make(chan string, 1)
	lp, err := lm.StreamLogsChannel(ch, containerName)
	require.NoError(t, err)
	require.NotNil(t, lp)

	assert.Equal(t, containerName, lp.ContainerName)
	assert.Equal(t, ch, lp.ChannelStream)
	assert.False(t, lp.Publish)

	// It should have been registered as the active log for this container.
	lm.activeLogsMutex.Lock()
	registered := lm.activeLogs[containerName]
	lm.activeLogsMutex.Unlock()
	assert.Same(t, lp, registered)
}

func TestStreamLogsChannelEnablesPublishWhenSubscribed(t *testing.T) {
	lm, _, msg, _ := newTestManager(t)

	containerName := "prod_3_subbed"

	msg.SetCallResponse(string(topics.MetaProcLookupSubscription), messenger.Result{
		Arguments: []interface{}{uint64(555)},
	}, nil)

	ch := make(chan string, 1)
	lp, err := lm.StreamLogsChannel(ch, containerName)
	require.NoError(t, err)
	require.NotNil(t, lp)

	assert.True(t, lp.Publish)
	assert.Equal(t, "555", lp.SubscriptionID)
}

func TestSetMessenger(t *testing.T) {
	lm, _, _, _ := newTestManager(t)

	replacement := fakes.NewMessengerWithConfig(
		builders.NewTestConfigBuilder().WithSerialNumber("swapped-serial").Build(),
	)
	lm.SetMessenger(replacement)

	assert.Equal(t, "reswarm.logs.swapped-serial.prod_1_myapp", lm.buildTopic("prod_1_myapp"))
}

// Compile-time assertion: the fake messenger satisfies the messenger interface
// the LogManager depends on.
var _ messenger.Messenger = (*fakes.Messenger)(nil)
