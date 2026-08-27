package apps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reagent/common"
	containerpkg "reagent/container"
	"reagent/store"
	"reagent/testutil/builders"
	"reagent/testutil/mocks"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Update ordering: pull BEFORE teardown
//
// An update must not destroy the running version until the new release is fully
// downloaded. The 2026-08-27 field incident is the motivating failure: the old
// flow tore the app down first, then hit an unreachable registry (a wedged
// proxy on the edge PC), and a production QDS deployment sat dead for hours of
// login-timeout retries. With pull-first, every failure up to and including the
// download leaves the old version running and the update merely refused.
//
// The non-compose flow is asserted against the strict mocks.Container (call
// order recorded via Run hooks). The compose flow shells out through
// container.Compose, so it is driven end-to-end against a FAKE docker CLI
// (container.NewComposeWithBinary) that records its invocations to a file —
// covering the exact `pull` / `down` sequencing without a daemon.
//
// Helpers here are prefixed updOrd*; everything else (updRequest, updSeedAt,
// updSentUpdateStatuses, amHarness, wiredRunBuildSM, fwdDockerStream,
// fwdAllowLogs, notFoundErr) comes from the other files in this package.
// =============================================================================

func TestUpdateAppPullsBeforeTeardown(t *testing.T) {
	sm, mc, _, _, _ := wiredRunBuildSM(t)

	app := updSeedAt(t, sm, "pullfirst", common.RUNNING, "1.0.0")
	payload := updRequest("pullfirst", common.RUNNING, "1.0.0", "2.0.0")

	// The transition executes its container calls sequentially in one
	// goroutine, so plain slice appends are race-free.
	var seq []string

	mc.EXPECT().HandleRegistryLogins(mock.Anything).
		Run(func(map[string]common.DockerCredential) { seq = append(seq, "login") }).
		Return(nil).Once()
	mc.EXPECT().Pull(mock.Anything, mock.Anything, mock.Anything).
		Run(func(context.Context, string, containerpkg.PullOptions) { seq = append(seq, "pull") }).
		Return(fwdDockerStream(), nil).Once()

	// The OLD container exists and must only be removed after the pull.
	mc.EXPECT().GetContainer(mock.Anything, payload.ContainerName.Prod).
		Run(func(context.Context, string) { seq = append(seq, "get-container") }).
		Return(dockertypes.Container{ID: "old-ctr"}, nil).Once()
	mc.EXPECT().RemoveContainerByID(mock.Anything, "old-ctr", mock.Anything).
		Run(func(context.Context, string, map[string]interface{}) { seq = append(seq, "remove-container") }).
		Return(nil).Once()

	pollErrC := make(chan error, 1)
	pollErrC <- notFoundErr()
	close(pollErrC)
	stateC := make(chan containerpkg.ContainerState)
	mc.EXPECT().PollContainerState(mock.Anything, "old-ctr", mock.Anything).
		Return(stateC, pollErrC).Once()

	mc.EXPECT().RemoveImageByName(mock.Anything, payload.RegistryImageName.Prod, "1.0.0", mock.Anything).
		Run(func(context.Context, string, string, map[string]interface{}) { seq = append(seq, "remove-old-image") }).
		Return(nil).Once()

	fwdAllowLogs(mc)

	require.NoError(t, sm.updateApp(payload, app))

	assert.Equal(t,
		[]string{"login", "pull", "get-container", "remove-container", "remove-old-image"},
		seq,
		"the old container must outlive the registry login and the full download")
}

// A registry that cannot be reached (login fails or the pull errors) must
// refuse the update WITHOUT touching the old container. The strict mock proves
// the point structurally: no GetContainer / RemoveContainerByID expectation is
// registered, so any teardown attempt fails the test.
func TestUpdateAppRegistryFailureLeavesOldContainerUntouched(t *testing.T) {
	t.Run("login failure", func(t *testing.T) {
		sm, mc, _, _, _ := wiredRunBuildSM(t)

		app := updSeedAt(t, sm, "loginfail", common.RUNNING, "1.0.0")
		payload := updRequest("loginfail", common.RUNNING, "1.0.0", "2.0.0")

		mc.EXPECT().HandleRegistryLogins(mock.Anything).
			Return(fmt.Errorf("registry login to registry.test timed out after 10s")).Once()
		fwdAllowLogs(mc)

		err := sm.updateApp(payload, app)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")

		app.StateLock.Lock()
		version := app.Version
		app.StateLock.Unlock()
		assert.Equal(t, "1.0.0", version, "a refused update must not adopt the new version")
	})

	t.Run("pull failure", func(t *testing.T) {
		sm, mc, _, _, _ := wiredRunBuildSM(t)

		app := updSeedAt(t, sm, "pullfail", common.RUNNING, "1.0.0")
		payload := updRequest("pullfail", common.RUNNING, "1.0.0", "2.0.0")

		mc.EXPECT().HandleRegistryLogins(mock.Anything).Return(nil).Once()
		mc.EXPECT().Pull(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, fmt.Errorf("pull access denied")).Once()
		fwdAllowLogs(mc)

		err := sm.updateApp(payload, app)
		require.Error(t, err)

		app.StateLock.Lock()
		version := app.Version
		app.StateLock.Unlock()
		assert.Equal(t, "1.0.0", version)
	})
}

// =============================================================================
// Compose update lifecycle against a fake docker CLI
// =============================================================================

// updOrdComposeHarness builds the fake CLI + an amHarness-backed StateMachine
// ready to run updateComposeApp. pullExit is the exit code the fake CLI uses
// for `pull` invocations (0 = success).
func updOrdComposeHarness(t *testing.T, pullExit int) (*StateMachine, *mocks.Container, *store.AppStore, string) {
	t.Helper()

	am, mc, mockTunnel, st, _, cfg := amHarness(t)
	sm := am.StateMachine

	mockTunnel.EXPECT().TunnelCapable().Return(false).Maybe()

	dir := t.TempDir()
	callsFile := filepath.Join(dir, "calls.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\ncase \"$*\" in *pull*) [ %d -ne 0 ] && { echo \"pull failed\"; exit %d; };; esac\nexit 0\n",
		callsFile, pullExit, pullExit)
	binPath := filepath.Join(dir, "fake-docker")
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	fake := containerpkg.NewComposeWithBinary(cfg, binPath)
	mc.EXPECT().Compose().Return(fake).Maybe()
	// SetupComposeFiles recovers already-published host ports via the API.
	mc.EXPECT().GetComposePublishedPorts(mock.Anything, mock.Anything).Return(map[string]uint64{}, nil).Maybe()
	fwdAllowLogs(mc)

	return sm, mc, st, callsFile
}

// updOrdComposeApp seeds a compose app + returns the update payload moving it
// from 1.0.0 to 2.0.0 with a changed service definition.
func updOrdComposeApp(t *testing.T, st *store.AppStore, name string) (*common.App, common.TransitionPayload) {
	t.Helper()

	oldCompose := map[string]interface{}{
		"services": map[string]interface{}{"app": map[string]interface{}{"image": "registry.test/x:1.0.0"}},
	}
	newCompose := map[string]interface{}{
		"services": map[string]interface{}{"app": map[string]interface{}{"image": "registry.test/x:2.0.0"}},
	}

	seed := builders.BuildTransitionPayload(name, common.RUNNING, common.PROD)
	seed.AppKey = 1
	seed.CurrentState = common.RUNNING
	seed.DockerCompose = oldCompose
	app, err := st.AddApp(seed)
	require.NoError(t, err)
	require.NotNil(t, app)
	app.StateLock.Lock()
	app.Version = "1.0.0"
	app.StateLock.Unlock()

	payload := updRequest(name, common.RUNNING, "1.0.0", "2.0.0")
	payload.DockerCompose = oldCompose
	payload.NewDockerCompose = newCompose
	return app, payload
}

// updOrdCalls reads the fake CLI's invocation log; a missing file means the CLI
// was never invoked at all.
func updOrdCalls(t *testing.T, callsFile string) []string {
	t.Helper()

	raw, err := os.ReadFile(callsFile)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestUpdateComposeAppPullsBeforeTeardown(t *testing.T) {
	sm, _, st, callsFile := updOrdComposeHarness(t, 0)

	mcc := sm.Container.(*mocks.Container)
	mcc.EXPECT().HandleRegistryLogins(mock.Anything).Return(nil).Once()

	app, payload := updOrdComposeApp(t, st, "cpullfirst")

	require.NoError(t, sm.updateComposeApp(payload, app))

	calls := updOrdCalls(t, callsFile)
	require.Len(t, calls, 2, "exactly one pull and one down expected, got: %v", calls)
	assert.Contains(t, calls[0], "pull", "the download must run first")
	assert.Contains(t, calls[1], "down", "teardown only after every image is local")
	assert.Contains(t, calls[1], "--remove-orphans",
		"the teardown must remove containers of services dropped in the new compose")

	// The successful update's bookkeeping (mirrors the non-compose test).
	app.StateLock.Lock()
	version := app.Version
	app.StateLock.Unlock()
	assert.Equal(t, "2.0.0", version)

	stored, err := st.GetRequestedState(1, common.PROD)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", stored.PresentVersion)
	assert.Equal(t, "2.0.0", stored.NewestVersion)
}

func TestUpdateComposeAppRegistryFailureLeavesProjectUntouched(t *testing.T) {
	t.Run("login failure: the CLI is never invoked", func(t *testing.T) {
		sm, _, st, callsFile := updOrdComposeHarness(t, 0)

		mcc := sm.Container.(*mocks.Container)
		mcc.EXPECT().HandleRegistryLogins(mock.Anything).
			Return(fmt.Errorf("registry login to registry.test timed out after 10s")).Once()

		app, payload := updOrdComposeApp(t, st, "cloginfail")

		err := sm.updateComposeApp(payload, app)
		require.Error(t, err)

		assert.Empty(t, updOrdCalls(t, callsFile),
			"an unreachable registry must abort the update before any compose command — the old project keeps running")

		app.StateLock.Lock()
		version := app.Version
		app.StateLock.Unlock()
		assert.Equal(t, "1.0.0", version)
	})

	t.Run("pull failure: no teardown happens", func(t *testing.T) {
		sm, _, st, callsFile := updOrdComposeHarness(t, 1)

		mcc := sm.Container.(*mocks.Container)
		mcc.EXPECT().HandleRegistryLogins(mock.Anything).Return(nil).Once()

		app, payload := updOrdComposeApp(t, st, "cpullfail")

		err := sm.updateComposeApp(payload, app)
		require.Error(t, err)

		calls := updOrdCalls(t, callsFile)
		require.NotEmpty(t, calls, "the pull must have been attempted")
		for _, call := range calls {
			assert.NotContains(t, call, "down",
				"a failed download must never tear the running project down")
		}

		app.StateLock.Lock()
		version := app.Version
		app.StateLock.Unlock()
		assert.Equal(t, "1.0.0", version)
	})
}

// =============================================================================
// Crashloop convergence with pull-first updates
//
// A failed pull-first update leaves the old version RUNNING; the observer
// corrects the FAILED blip back to RUNNING — which is exactly the state the
// update was requested from. crashLoopWake's "reached the target state" exit
// must therefore consult the row's pending update, or the retry loop ends with
// the update silently dropped.
// =============================================================================

func TestCrashLoopWakeKeepsRetryingAPendingUpdateAtItsTargetState(t *testing.T) {
	am, _, mockTunnel, st, _, _ := amHarness(t)
	mockTunnel.EXPECT().TunnelCapable().Return(false).Maybe()

	app := amSeed(t, st, 71, "pendingupd", common.RUNNING, common.PROD)

	// The requested-state row still carries the un-collapsed update.
	row := amPayload(71, "pendingupd", common.RUNNING, common.PROD)
	row.RequestUpdate = true
	row.PresentVersion = "1.0.0"
	row.NewestVersion = "2.0.0"
	require.NoError(t, st.UpdateLocalRequestedState(row))

	captured := row
	captured.Retrying = true
	task := &CrashLoop{Payload: captured}
	am.crashLoopLock.Lock()
	am.crashLoops[task] = struct{}{}
	am.crashLoopLock.Unlock()

	// Pre-lock the app so the wake's RequestAppState is dropped at the
	// transition lock: the test observes the wake's DECISION (loop kept,
	// retry dispatched) without running a whole update transition.
	require.False(t, app.SecureTransition())

	am.crashLoopWake(task)

	assert.True(t, am.hasActiveCrashLoop(71, common.PROD),
		"reaching the target STATE must not end the loop while the row still carries the update")
}

func TestCrashLoopWakeClearsALoopAtItsTargetStateWithNoPendingUpdate(t *testing.T) {
	am, _, mockTunnel, st, _, _ := amHarness(t)
	mockTunnel.EXPECT().TunnelCapable().Return(false).Maybe()

	amSeed(t, st, 72, "settled", common.RUNNING, common.PROD)

	row := amPayload(72, "settled", common.RUNNING, common.PROD)
	require.NoError(t, st.UpdateLocalRequestedState(row))

	captured := row
	captured.Retrying = true
	task := &CrashLoop{Payload: captured}
	am.crashLoopLock.Lock()
	am.crashLoops[task] = struct{}{}
	am.crashLoopLock.Unlock()

	am.crashLoopWake(task)

	assert.False(t, am.hasActiveCrashLoop(72, common.PROD),
		"at the target state with nothing pending the loop must end (existing behavior)")
}

// =============================================================================
// The helpers the observer guard is built from
// =============================================================================

func TestHasPendingUpdate(t *testing.T) {
	base := amPayload(80, "helper", common.RUNNING, common.PROD)

	pending := base
	pending.RequestUpdate = true
	pending.PresentVersion = "1.0.0"
	pending.NewestVersion = "2.0.0"
	assert.True(t, hasPendingUpdate(pending))

	collapsed := pending
	collapsed.PresentVersion = "2.0.0"
	assert.False(t, hasPendingUpdate(collapsed), "collapsed versions mean the update completed")

	notRequested := pending
	notRequested.RequestUpdate = false
	assert.False(t, hasPendingUpdate(notRequested))

	uninstalling := pending
	uninstalling.RequestedState = common.UNINSTALLED
	assert.False(t, hasPendingUpdate(uninstalling), "an uninstall wins over a pending update")
}

func TestHasActiveCrashLoop(t *testing.T) {
	am, _, _, _, _, _ := amHarness(t)

	assert.False(t, am.hasActiveCrashLoop(90, common.PROD))

	task := &CrashLoop{Payload: amPayload(90, "loop", common.RUNNING, common.PROD)}
	am.crashLoopLock.Lock()
	am.crashLoops[task] = struct{}{}
	am.crashLoopLock.Unlock()

	assert.True(t, am.hasActiveCrashLoop(90, common.PROD))
	assert.False(t, am.hasActiveCrashLoop(90, common.DEV), "stage is part of the identity")
	assert.False(t, am.hasActiveCrashLoop(91, common.PROD))

	am.clearCrashLoop(90, common.PROD)
	assert.False(t, am.hasActiveCrashLoop(90, common.PROD))
}
