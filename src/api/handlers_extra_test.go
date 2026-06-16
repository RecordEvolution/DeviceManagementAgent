package api

import (
	"context"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/errdefs"
	"reagent/logging"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/persistence"
	"reagent/store"
	"reagent/testutil/builders"
	"reagent/testutil/fakes"
	"reagent/testutil/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Shared wiring for the LogManager-backed handlers
//
// getAppLogHistoryHandler reads from a real LogManager whose persistence layer
// is an isolated, temp-file SQLite DB (pure-Go driver, no external service) and
// whose Container is a strict mock. This mirrors logging/applog_test.go's
// newTestManager so the handler exercises the same store/DB path.
// =============================================================================

// newLogDB builds a real, isolated SQLite-backed persistence DB in a temp dir
// with the schema initialized. Cleanup closes it.
func newLogDB(t *testing.T) persistence.Database {
	t.Helper()

	cfg := builders.DefaultTestConfig()
	cfg.CommandLineArguments.DatabaseFileName = filepath.Join(t.TempDir(), "reagent_test.db")

	db, err := persistence.NewSQLiteDb(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Init())

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newLogManagerEx wires an External whose LogManager is backed by a real DB +
// store, a fake messenger, and a strict container mock (no expectations by
// default). The returned db lets a test seed persisted log history.
func newLogManagerEx(t *testing.T, granted bool) (*External, *mocks.Container, persistence.Database) {
	t.Helper()

	db := newLogDB(t)
	msg := fakes.NewMessenger()
	cont := mocks.NewContainer(t)
	as := store.NewAppStore(db, msg)
	lm := logging.NewLogManager(cont, msg, db, as)

	ex := &External{
		LogManager: &lm,
		Privilege:  priv(t, granted),
	}
	return ex, cont, db
}

// seedApp inserts an app into the store-backed DB so getPersistedLogHistory
// finds a non-nil app (and thus consults the LogHistory table) for the given
// container coordinates.
func seedApp(t *testing.T, db persistence.Database, appKey uint64, appName string, stage common.Stage) {
	t.Helper()
	app := builders.BuildApp(appName, common.RUNNING, stage)
	app.AppKey = appKey
	_, err := db.UpsertAppState(app, common.RUNNING)
	require.NoError(t, err)
}

// =============================================================================
// getAppLogHistoryHandler - reads persisted app logs via the LogManager
// =============================================================================

func TestGetAppLogHistoryHandler(t *testing.T) {
	t.Run("returns persisted container history when privileged", func(t *testing.T) {
		ex, _, db := newLogManagerEx(t, true)

		// prod_1_logapp parses to stage=PROD, appKey=1, name=logapp.
		seedApp(t, db, 1, "logapp", common.PROD)
		require.NoError(t, db.UpsertLogHistory("logapp", 1, common.PROD, []string{"line-1", "line-2"}))

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: []interface{}{map[string]interface{}{"containerName": "prod_1_logapp"}},
		})

		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Arguments, 1)

		history, ok := res.Arguments[0].([]string)
		require.True(t, ok)
		assert.Equal(t, []string{"line-1", "line-2"}, history)
	})

	t.Run("propagates error for an unparseable container name", func(t *testing.T) {
		// "not-valid" fails common.ParseContainerName inside GetLogHistory, so
		// the handler surfaces the error before any container call.
		ex, _, _ := newLogManagerEx(t, true)

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: []interface{}{map[string]interface{}{"containerName": "not-valid"}},
		})

		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("rejects nil args", func(t *testing.T) {
		ex, _, _ := newLogManagerEx(t, true)

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: nil,
		})

		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("rejects empty args", func(t *testing.T) {
		ex, _, _ := newLogManagerEx(t, true)

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: []interface{}{nil},
		})

		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("rejects non-dict first arg", func(t *testing.T) {
		ex, _, _ := newLogManagerEx(t, true)

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: []interface{}{"not-a-dict"},
		})

		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("rejects bad containerName type", func(t *testing.T) {
		ex, _, _ := newLogManagerEx(t, true)

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   systemDetails(),
			Arguments: []interface{}{map[string]interface{}{"containerName": 123}},
		})

		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("denies unprivileged caller", func(t *testing.T) {
		// The strict container mock and the LogManager must not be touched when
		// the privilege gate rejects the caller.
		details, m := grantPrivilege(false)
		cont := mocks.NewContainer(t)
		ex := &External{
			LogManager: nil,
			Container:  cont,
			Privilege:  newPrivilege(testConfig(), m),
		}

		res, err := ex.getAppLogHistoryHandler(context.Background(), messenger.Result{
			Details:   details,
			Arguments: []interface{}{map[string]interface{}{"containerName": "prod_1_logapp"}},
		})

		require.Error(t, err)
		assert.Nil(t, res)
		assert.True(t, errdefs.IsInsufficientPrivileges(err))
	})
}

// =============================================================================
// getAgentLogs - reads the reagent log file from disk into a Dict
//
// No privilege gate: the handler just reads Config.LogFileLocation and shapes
// the contents under the "reagent.log" key. A missing file is a soft case.
// =============================================================================

func TestGetAgentLogs(t *testing.T) {
	t.Run("returns the log file contents under reagent.log", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "reagent.log")
		const contents = "boot line 1\nboot line 2\n"
		require.NoError(t, os.WriteFile(logFile, []byte(contents), 0o644))

		cfg := testConfig()
		cfg.CommandLineArguments.LogFileLocation = logFile
		ex := &External{Config: cfg}

		res, err := ex.getAgentLogs(context.Background(), messenger.Result{
			Details: systemDetails(),
		})

		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Arguments, 1)

		dict, ok := res.Arguments[0].(common.Dict)
		require.True(t, ok)
		assert.Equal(t, contents, dict["reagent.log"])
	})

	t.Run("reports a placeholder when the log file is missing", func(t *testing.T) {
		// Point at a path that does not exist; os.IsNotExist branch fills a
		// placeholder string instead of erroring.
		cfg := testConfig()
		cfg.CommandLineArguments.LogFileLocation = filepath.Join(t.TempDir(), "does-not-exist.log")
		ex := &External{Config: cfg}

		res, err := ex.getAgentLogs(context.Background(), messenger.Result{
			Details: systemDetails(),
		})

		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Arguments, 1)

		dict, ok := res.Arguments[0].(common.Dict)
		require.True(t, ok)
		assert.Equal(t, "log file was not found", dict["reagent.log"])
	})
}

// =============================================================================
// codeExecutionHandler - argument parsing/validation (no exec reached)
//
// The handler has no privilege gate; it validates the args dict and only then
// builds an exec.Command. Every case below fails parsing before exec.Command,
// so no system process is started.
// =============================================================================

func TestCodeExecutionHandlerValidation(t *testing.T) {
	cases := []struct {
		name string
		args []interface{}
	}{
		{
			name: "nil args",
			args: nil,
		},
		{
			name: "empty first arg",
			args: []interface{}{nil},
		},
		{
			name: "non-dict first arg",
			args: []interface{}{"not-a-dict"},
		},
		{
			name: "missing cmd",
			args: []interface{}{map[string]interface{}{"blocking": true}},
		},
		{
			name: "bad cmd type",
			args: []interface{}{map[string]interface{}{"cmd": 123, "blocking": true}},
		},
		{
			name: "missing blocking",
			args: []interface{}{map[string]interface{}{"cmd": "echo"}},
		},
		{
			name: "bad blocking type",
			args: []interface{}{map[string]interface{}{"cmd": "echo", "blocking": "yes"}},
		},
		{
			name: "bad args array type",
			args: []interface{}{map[string]interface{}{
				"cmd": "echo", "blocking": true, "args": "not-an-array",
			}},
		},
		{
			name: "bad timeout type",
			args: []interface{}{map[string]interface{}{
				"cmd": "echo", "blocking": false, "timeout": "soon",
			}},
		},
	}

	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			// No Container/Privilege needed: parsing fails before any of them is used.
			ex := &External{}

			res, err := ex.codeExecutionHandler(context.Background(), messenger.Result{
				Details:   systemDetails(),
				Arguments: tc.args,
			})

			require.Error(t, err)
			assert.Nil(t, res)
		})
	}
}

// =============================================================================
// update_system handlers - privilege gate (no system exec reached)
//
// Each handler checks ex.Privilege before touching the real OS/system layer,
// so a denied caller takes the early-return path with System left nil.
// =============================================================================

func TestUpdateSystemPrivilegeGate(t *testing.T) {
	handlers := []struct {
		name string
		call func(ex *External) (*messenger.InvokeResult, error)
	}{
		{
			name: "getOSReleaseHandler",
			call: func(ex *External) (*messenger.InvokeResult, error) {
				return ex.getOSReleaseHandler(context.Background(), messenger.Result{
					Details: common.Dict{"caller_authid": "999"},
				})
			},
		},
		{
			name: "downloadOSUpdateHandler",
			call: func(ex *External) (*messenger.InvokeResult, error) {
				return ex.downloadOSUpdateHandler(context.Background(), messenger.Result{
					Details: common.Dict{"caller_authid": "999"},
				})
			},
		},
		{
			name: "installOSUpdateHandler",
			call: func(ex *External) (*messenger.InvokeResult, error) {
				return ex.installOSUpdateHandler(context.Background(), messenger.Result{
					Details: common.Dict{"caller_authid": "999"},
				})
			},
		},
	}

	for _, h := range handlers {
		t.Run(h.name+" denies unprivileged caller", func(t *testing.T) {
			_, m := grantPrivilege(false)
			ex := &External{Config: testConfig(), Privilege: newPrivilege(testConfig(), m)}

			res, err := h.call(ex)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.True(t, errdefs.IsInsufficientPrivileges(err))
		})

		t.Run(h.name+" propagates privilege check error", func(t *testing.T) {
			m := fakes.NewMessenger()
			m.SetCallError(string(topics.CheckPrivilege), assert.AnError)
			ex := &External{Config: testConfig(), Privilege: newPrivilege(testConfig(), m)}

			res, err := h.call(ex)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.False(t, errdefs.IsInsufficientPrivileges(err))
		})
	}
}

// =============================================================================
// terminal_management handlers - privilege gate + payload parsing
//
// TerminalManager is a concrete struct (not an interface), so it cannot be
// mocked. Every case below either fails the privilege check or fails payload
// parsing, both of which return BEFORE ex.TerminalManager is dereferenced, so
// it stays nil safely.
// =============================================================================

func TestTerminalManagementPrivilegeGate(t *testing.T) {
	denials := []struct {
		name string
		call func(ex *External, r messenger.Result) (*messenger.InvokeResult, error)
	}{
		{
			name: "startTerminalSessHandler",
			call: func(ex *External, r messenger.Result) (*messenger.InvokeResult, error) {
				return ex.startTerminalSessHandler(context.Background(), r)
			},
		},
		{
			name: "stopTerminalSession",
			call: func(ex *External, r messenger.Result) (*messenger.InvokeResult, error) {
				return ex.stopTerminalSession(context.Background(), r)
			},
		},
		{
			name: "requestTerminalSessHandler",
			call: func(ex *External, r messenger.Result) (*messenger.InvokeResult, error) {
				return ex.requestTerminalSessHandler(context.Background(), r)
			},
		},
	}

	for _, d := range denials {
		t.Run(d.name+" denies unprivileged caller", func(t *testing.T) {
			details, m := grantPrivilege(false)
			ex := &External{Privilege: newPrivilege(testConfig(), m)}

			res, err := d.call(ex, messenger.Result{
				Details:   details,
				Arguments: []interface{}{map[string]interface{}{"sessionID": "s"}},
			})

			require.Error(t, err)
			assert.Nil(t, res)
			assert.True(t, errdefs.IsInsufficientPrivileges(err))
		})
	}
}

func TestStartTerminalSessHandlerParsing(t *testing.T) {
	cases := []struct {
		name string
		args []interface{}
	}{
		{name: "nil args", args: nil},
		{name: "non-dict payload", args: []interface{}{"not-a-dict"}},
		{name: "missing sessionID", args: []interface{}{map[string]interface{}{
			"registrationID": uint64(7),
		}}},
		{name: "bad sessionID type", args: []interface{}{map[string]interface{}{
			"sessionID": 1, "registrationID": uint64(7),
		}}},
		{name: "missing registrationID", args: []interface{}{map[string]interface{}{
			"sessionID": "s",
		}}},
		{name: "bad registrationID type", args: []interface{}{map[string]interface{}{
			"sessionID": "s", "registrationID": "seven",
		}}},
	}

	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			ex := &External{Privilege: priv(t, true)}

			res, err := ex.startTerminalSessHandler(context.Background(), messenger.Result{
				Details:   systemDetails(),
				Arguments: tc.args,
			})

			require.Error(t, err)
			assert.Nil(t, res)
		})
	}
}

func TestStopTerminalSessionParsing(t *testing.T) {
	cases := []struct {
		name string
		args []interface{}
	}{
		{name: "nil args", args: nil},
		{name: "non-dict payload", args: []interface{}{42}},
		{name: "missing sessionID", args: []interface{}{map[string]interface{}{}}},
		{name: "bad sessionID type", args: []interface{}{map[string]interface{}{"sessionID": 1}}},
	}

	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			ex := &External{Privilege: priv(t, true)}

			res, err := ex.stopTerminalSession(context.Background(), messenger.Result{
				Details:   systemDetails(),
				Arguments: tc.args,
			})

			require.Error(t, err)
			assert.Nil(t, res)
		})
	}
}

func TestRequestTerminalSessHandlerParsing(t *testing.T) {
	cases := []struct {
		name string
		args []interface{}
	}{
		{name: "nil args", args: nil},
		{name: "non-dict payload", args: []interface{}{"nope"}},
		{name: "missing containerName", args: []interface{}{map[string]interface{}{}}},
		{name: "bad containerName type", args: []interface{}{map[string]interface{}{"containerName": 9}}},
	}

	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			ex := &External{Privilege: priv(t, true)}

			res, err := ex.requestTerminalSessHandler(context.Background(), messenger.Result{
				Details:   systemDetails(),
				Arguments: tc.args,
			})

			require.Error(t, err)
			assert.Nil(t, res)
		})
	}
}
