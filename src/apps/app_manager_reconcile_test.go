package apps

import (
	"testing"

	"reagent/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// app_manager.go — local DB reconciliation helpers
//
// These exercise the pure store-backed reconciliation methods (no docker /
// tunnel / network), reusing the amHarness AppManager from app_manager_test.go.
// =============================================================================

func TestCreateOrUpdateApp(t *testing.T) {
	t.Run("creates a new app row and records the requested state", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		payload := amPayload(10, "new-app", common.RUNNING, common.PROD)
		payload.RequestedState = common.RUNNING

		err := am.CreateOrUpdateApp(payload)
		require.NoError(t, err)

		app, err := st.GetApp(10, common.PROD)
		require.NoError(t, err)
		require.NotNil(t, app)
		app.StateLock.Lock()
		reqState := app.RequestedState
		app.StateLock.Unlock()
		// RUNNING is a stable actual state -> TransientToActualState keeps it.
		assert.Equal(t, common.RUNNING, reqState)

		// And the requested-state row was persisted.
		got, err := st.GetRequestedState(10, common.PROD)
		require.NoError(t, err)
		assert.Equal(t, common.RUNNING, got.RequestedState)
	})

	t.Run("normalizes a transient requested state to its actual target", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		// DOWNLOADING is transient -> normalized to PRESENT.
		payload := amPayload(11, "transient-app", common.DOWNLOADING, common.PROD)
		payload.RequestedState = common.DOWNLOADING

		err := am.CreateOrUpdateApp(payload)
		require.NoError(t, err)

		app, err := st.GetApp(11, common.PROD)
		require.NoError(t, err)
		require.NotNil(t, app)
		app.StateLock.Lock()
		reqState := app.RequestedState
		app.StateLock.Unlock()
		assert.Equal(t, common.PRESENT, reqState)
	})

	t.Run("does not change the requested state of an already-BUILT app awaiting promotion", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		// The guard fires when the app's CurrentState is BUILT and its existing
		// RequestedState is NOT BUILT (it has finished building and is waiting to
		// be promoted to PRESENT). A new requested state must not clobber it.
		app := amSeed(t, st, 12, "built-app", common.BUILT, common.DEV)
		app.StateLock.Lock()
		app.RequestedState = common.PRESENT
		app.StateLock.Unlock()

		payload := amPayload(12, "built-app", common.RUNNING, common.DEV)
		payload.RequestedState = common.RUNNING

		err := am.CreateOrUpdateApp(payload)
		require.NoError(t, err)

		app.StateLock.Lock()
		reqState := app.RequestedState
		app.StateLock.Unlock()
		assert.Equal(t, common.PRESENT, reqState, "requested state should be left untouched")
	})
}

func TestUpdateCurrentAppState(t *testing.T) {
	t.Run("persists the in-memory current state without changing a RUNNING app", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		amSeed(t, st, 20, "cur-app", common.RUNNING, common.PROD)

		payload := amPayload(20, "cur-app", common.RUNNING, common.PROD)
		// A RUNNING app is not BUILT/PUBLISHED, so CurrentState is left untouched
		// even though the payload carries one.
		payload.CurrentState = common.PRESENT

		err := am.UpdateCurrentAppState(payload)
		require.NoError(t, err)

		got, err := st.GetApp(20, common.PROD)
		require.NoError(t, err)
		got.StateLock.Lock()
		cur := got.CurrentState
		got.StateLock.Unlock()
		assert.Equal(t, common.RUNNING, cur)
	})

	t.Run("adopts the payload current state when the app is BUILT", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		app := amSeed(t, st, 21, "built-cur", common.BUILT, common.DEV)
		app.StateLock.Lock()
		app.RequestedState = common.RUNNING
		app.StateLock.Unlock()

		payload := amPayload(21, "built-cur", common.RUNNING, common.DEV)
		payload.CurrentState = common.RUNNING
		payload.PresentVersion = "9.9.9"

		err := am.UpdateCurrentAppState(payload)
		require.NoError(t, err)

		app.StateLock.Lock()
		cur := app.CurrentState
		ver := app.Version
		app.StateLock.Unlock()
		assert.Equal(t, common.RUNNING, cur, "a BUILT app adopts the payload's current state")
		assert.Equal(t, "9.9.9", ver, "the present version is applied")
	})
}

func TestUpdateLocalRequestedAppStatesWithRemote(t *testing.T) {
	t.Run("creates prod apps from the remote payloads and ignores dev", func(t *testing.T) {
		am, _, _, st, _, _ := amHarness(t)

		prod := amPayload(30, "remote-prod", common.RUNNING, common.PROD)
		prod.RequestedState = common.RUNNING
		dev := amPayload(31, "remote-dev", common.BUILT, common.DEV)
		dev.RequestedState = common.BUILT

		err := am.UpdateLocalRequestedAppStatesWithRemote([]common.TransitionPayload{prod, dev})
		require.NoError(t, err)

		// The prod app now exists locally.
		gotProd, err := st.GetApp(30, common.PROD)
		require.NoError(t, err)
		require.NotNil(t, gotProd)

		// The dev app was skipped entirely (no row created).
		gotDev, err := st.GetApp(31, common.DEV)
		require.NoError(t, err)
		assert.Nil(t, gotDev, "dev payloads are not materialized by this method")
	})

	t.Run("empty remote list is a clean no-op", func(t *testing.T) {
		am, _, _, _, _, _ := amHarness(t)

		err := am.UpdateLocalRequestedAppStatesWithRemote([]common.TransitionPayload{})
		require.NoError(t, err)
	})
}

func TestHandleTransferFailure(t *testing.T) {
	t.Run("resets a TRANSFERING app to REMOVED via the observer", func(t *testing.T) {
		am, _, _, st, msg, _ := amHarness(t)

		app := amSeed(t, st, 40, "transfer-app", common.TRANSFERING, common.DEV)
		containerName := common.BuildContainerName(common.DEV, 40, "transfer-app")

		am.HandleTransferFailure(containerName, assertErr("transfer canceled"))

		app.StateLock.Lock()
		final := app.CurrentState
		app.StateLock.Unlock()
		assert.Equal(t, common.REMOVED, final)

		states := remoteStatesFor(msg)
		assert.Contains(t, states, common.REMOVED)
	})

	t.Run("does nothing when the app is not in TRANSFERING state", func(t *testing.T) {
		am, _, _, st, msg, _ := amHarness(t)

		app := amSeed(t, st, 41, "running-app", common.RUNNING, common.DEV)
		containerName := common.BuildContainerName(common.DEV, 41, "running-app")

		before := len(msg.CallCalls)
		am.HandleTransferFailure(containerName, assertErr("transfer canceled"))

		app.StateLock.Lock()
		final := app.CurrentState
		app.StateLock.Unlock()
		assert.Equal(t, common.RUNNING, final, "a non-TRANSFERING app is left untouched")
		assert.Equal(t, before, len(msg.CallCalls), "no remote notification should be sent")
	})

	t.Run("is a no-op for an unknown container", func(t *testing.T) {
		am, _, _, _, _, _ := amHarness(t)

		// No app seeded -> GetAppByContainerName returns nil -> early return.
		am.HandleTransferFailure("dev_999_nope", assertErr("transfer canceled"))
	})
}

// assertErr is a tiny local error constructor (kept distinct from the
// notFoundErr/boom helpers used elsewhere in the package's tests).
func assertErr(msg string) error { return &simpleErr{msg} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }
