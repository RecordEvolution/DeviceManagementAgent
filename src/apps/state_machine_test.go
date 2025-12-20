package apps

import (
	"reagent/common"
	"reagent/config"
	"reagent/container"
	"reagent/store"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

// =============================================================================
// Test Setup Helpers
// =============================================================================

func testConfig() *config.Config {
	return &config.Config{
		CommandLineArguments: &config.CommandLineArguments{
			AgentDir:       "/opt/reagent",
			AppsDirectory:  "/opt/reagent/apps",
			AppsBuildDir:   "/opt/reagent/apps/build",
			AppsComposeDir: "/opt/reagent/apps/compose",
			AppsSharedDir:  "/opt/reagent/apps/shared",
			DownloadDir:    "/opt/reagent/downloads",
			PrettyLogging:  true,
			Debug:          true,
		},
		ReswarmConfig: &config.ReswarmConfig{
			Environment:       string(common.PRODUCTION),
			DeviceKey:         12345,
			Secret:            "test-secret",
			DockerRegistryURL: "registry.test.com",
		},
	}
}

func setupTestStateMachine() (*StateMachine, *MockContainer, *StateObserver) {
	mockContainer := NewMockContainer()

	// Create a minimal AppStore for testing
	appStore := &store.AppStore{}

	// Create StateObserver
	observer := NewObserver(mockContainer, appStore, nil)

	// Create StateMachine
	sm := NewStateMachine(mockContainer, nil, &observer, nil)

	return &sm, mockContainer, &observer
}

func createTestApp(name string, currentState common.AppState, stage common.Stage) *common.App {
	return &common.App{
		AppKey:         1,
		AppName:        name,
		CurrentState:   currentState,
		RequestedState: common.PRESENT,
		Stage:          stage,
		Version:        "1.0.0",
		TransitionLock: semaphore.NewWeighted(1),
	}
}

func createTestPayload(appName string, requestedState common.AppState, stage common.Stage) common.TransitionPayload {
	return common.TransitionPayload{
		AppName:        appName,
		AppKey:         1,
		RequestedState: requestedState,
		Stage:          stage,
		Version:        "1.0.0",
		DockerCompose: map[string]any{
			"version": "3",
			"services": map[string]any{
				"app": map[string]any{
					"image": "test-image:latest",
				},
			},
		},
	}
}

// =============================================================================
// TestGetTransitionFunc - Tests for transition function lookup
// =============================================================================

func TestGetTransitionFunc(t *testing.T) {
	sm, _, _ := setupTestStateMachine()

	t.Run("returns function for valid REMOVED->PRESENT transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.REMOVED, common.PRESENT)
		assert.NotNil(t, transitionFunc, "Expected transition function for REMOVED->PRESENT")
	})

	t.Run("returns function for valid REMOVED->RUNNING transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.REMOVED, common.RUNNING)
		assert.NotNil(t, transitionFunc, "Expected transition function for REMOVED->RUNNING")
	})

	t.Run("returns function for valid RUNNING->PRESENT transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.RUNNING, common.PRESENT)
		assert.NotNil(t, transitionFunc, "Expected transition function for RUNNING->PRESENT")
	})

	t.Run("returns function for valid RUNNING->REMOVED transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.RUNNING, common.REMOVED)
		assert.NotNil(t, transitionFunc, "Expected transition function for RUNNING->REMOVED")
	})

	t.Run("returns function for valid PRESENT->RUNNING transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.PRESENT, common.RUNNING)
		assert.NotNil(t, transitionFunc, "Expected transition function for PRESENT->RUNNING")
	})

	t.Run("returns function for valid PRESENT->REMOVED transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.PRESENT, common.REMOVED)
		assert.NotNil(t, transitionFunc, "Expected transition function for PRESENT->REMOVED")
	})

	t.Run("returns noAction function for same state transition", func(t *testing.T) {
		transitionFunc := sm.getTransitionFunc(common.REMOVED, common.REMOVED)
		assert.NotNil(t, transitionFunc, "Expected noAction transition function for REMOVED->REMOVED")
	})

	t.Run("returns nil for undefined transition", func(t *testing.T) {
		// RUNNING->DOWNLOADING is not defined
		transitionFunc := sm.getTransitionFunc(common.RUNNING, common.DOWNLOADING)
		assert.Nil(t, transitionFunc, "Expected nil for undefined transition")
	})
}

// =============================================================================
// TestStateTransitionMatrix - Table-driven tests for state transitions
// =============================================================================

func TestStateTransitionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		fromState   common.AppState
		toState     common.AppState
		expectNil   bool
		description string
	}{
		// REMOVED state transitions
		{"REMOVED->PRESENT", common.REMOVED, common.PRESENT, false, "Pull app"},
		{"REMOVED->RUNNING", common.REMOVED, common.RUNNING, false, "Pull and run app"},
		{"REMOVED->BUILT", common.REMOVED, common.BUILT, false, "Build app"},
		{"REMOVED->PUBLISHED", common.REMOVED, common.PUBLISHED, false, "Publish app"},
		{"REMOVED->UNINSTALLED", common.REMOVED, common.UNINSTALLED, false, "Uninstall app"},
		{"REMOVED->REMOVED", common.REMOVED, common.REMOVED, false, "No action"},

		// UNINSTALLED state transitions
		{"UNINSTALLED->PRESENT", common.UNINSTALLED, common.PRESENT, false, "Pull app"},
		{"UNINSTALLED->RUNNING", common.UNINSTALLED, common.RUNNING, false, "Run app"},
		{"UNINSTALLED->BUILT", common.UNINSTALLED, common.BUILT, false, "Build app"},
		{"UNINSTALLED->UNINSTALLED", common.UNINSTALLED, common.UNINSTALLED, false, "No action"},

		// PRESENT state transitions
		{"PRESENT->REMOVED", common.PRESENT, common.REMOVED, false, "Remove app"},
		{"PRESENT->UNINSTALLED", common.PRESENT, common.UNINSTALLED, false, "Uninstall app"},
		{"PRESENT->RUNNING", common.PRESENT, common.RUNNING, false, "Run app"},
		{"PRESENT->BUILT", common.PRESENT, common.BUILT, false, "Build app"},
		{"PRESENT->PUBLISHED", common.PRESENT, common.PUBLISHED, false, "Publish app"},
		{"PRESENT->PRESENT", common.PRESENT, common.PRESENT, false, "No action"},

		// RUNNING state transitions
		{"RUNNING->RUNNING", common.RUNNING, common.RUNNING, false, "No action"},
		{"RUNNING->PRESENT", common.RUNNING, common.PRESENT, false, "Stop app"},
		{"RUNNING->REMOVED", common.RUNNING, common.REMOVED, false, "Remove app"},
		{"RUNNING->UNINSTALLED", common.RUNNING, common.UNINSTALLED, false, "Uninstall app"},

		// FAILED state transitions
		{"FAILED->REMOVED", common.FAILED, common.REMOVED, false, "Remove app"},
		{"FAILED->UNINSTALLED", common.FAILED, common.UNINSTALLED, false, "Uninstall app"},
		{"FAILED->PRESENT", common.FAILED, common.PRESENT, false, "Recover to present"},
		{"FAILED->RUNNING", common.FAILED, common.RUNNING, false, "Recover to running"},

		// DOWNLOADING state transitions
		{"DOWNLOADING->PRESENT", common.DOWNLOADING, common.PRESENT, false, "Continue/retry pull"},
		{"DOWNLOADING->REMOVED", common.DOWNLOADING, common.REMOVED, false, "Cancel pull"},
		{"DOWNLOADING->UNINSTALLED", common.DOWNLOADING, common.UNINSTALLED, false, "Cancel pull"},

		// STOPPED state transitions
		{"STOPPED->REMOVED", common.STOPPED, common.REMOVED, false, "Remove app"},
		{"STOPPED->RUNNING", common.STOPPED, common.RUNNING, false, "Run app"},
		{"STOPPED->STOPPED", common.STOPPED, common.STOPPED, false, "No action"},

		// Invalid/undefined transitions (should return nil)
		{"RUNNING->DOWNLOADING", common.RUNNING, common.DOWNLOADING, true, "Not defined"},
		{"PRESENT->STOPPED", common.PRESENT, common.STOPPED, true, "Not defined"},
	}

	sm, _, _ := setupTestStateMachine()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transitionFunc := sm.getTransitionFunc(tt.fromState, tt.toState)
			if tt.expectNil {
				assert.Nil(t, transitionFunc, "Expected nil for transition %s", tt.name)
			} else {
				assert.NotNil(t, transitionFunc, "Expected function for transition %s (%s)", tt.name, tt.description)
			}
		})
	}
}

// =============================================================================
// TestNoActionTransition - Tests for no-op transitions
// =============================================================================

func TestNoActionTransition(t *testing.T) {
	sm, _, _ := setupTestStateMachine()

	t.Run("noActionTransitionFunc returns NoActionTransition error", func(t *testing.T) {
		app := createTestApp("test-app", common.PRESENT, common.PROD)
		payload := createTestPayload("test-app", common.PRESENT, common.PROD)

		err := sm.noActionTransitionFunc(payload, app)

		require.Error(t, err)
		// The error should be a NoActionTransition type
		assert.Contains(t, err.Error(), "no action")
	})
}

// =============================================================================
// TestTransientStates - Tests for transient state helper functions
// =============================================================================

func TestTransientStates(t *testing.T) {
	t.Run("IsTransientState returns true for transient states", func(t *testing.T) {
		transientStates := []common.AppState{
			common.BUILDING,
			common.BUILT,
			common.TRANSFERED,
			common.DELETING,
			common.TRANSFERING,
			common.PUBLISHING,
			common.PUBLISHED,
			common.DOWNLOADING,
			common.STOPPING,
			common.UPDATING,
			common.STARTING,
		}

		for _, state := range transientStates {
			assert.True(t, common.IsTransientState(state), "Expected %s to be transient", state)
		}
	})

	t.Run("IsTransientState returns false for stable states", func(t *testing.T) {
		stableStates := []common.AppState{
			common.PRESENT,
			common.REMOVED,
			common.RUNNING,
			common.FAILED,
		}

		for _, state := range stableStates {
			assert.False(t, common.IsTransientState(state), "Expected %s to be stable (not transient)", state)
		}
	})
}

// =============================================================================
// TestCancelableStates - Tests for cancelable state helper functions
// =============================================================================

func TestCancelableStates(t *testing.T) {
	t.Run("IsCancelableState returns true for cancelable states", func(t *testing.T) {
		cancelableStates := []common.AppState{
			common.BUILDING,
			common.PUBLISHING,
			common.DOWNLOADING,
		}

		for _, state := range cancelableStates {
			assert.True(t, common.IsCancelableState(state), "Expected %s to be cancelable", state)
		}
	})

	t.Run("IsCancelableState returns false for non-cancelable states", func(t *testing.T) {
		nonCancelableStates := []common.AppState{
			common.PRESENT,
			common.REMOVED,
			common.RUNNING,
			common.STARTING,
			common.STOPPING,
		}

		for _, state := range nonCancelableStates {
			assert.False(t, common.IsCancelableState(state), "Expected %s to not be cancelable", state)
		}
	})
}

// =============================================================================
// TestAppIsCancelable - Tests for App.IsCancelable method
// =============================================================================

func TestAppIsCancelable(t *testing.T) {
	t.Run("returns true when app is in cancelable state", func(t *testing.T) {
		app := createTestApp("test-app", common.DOWNLOADING, common.PROD)
		assert.True(t, app.IsCancelable())
	})

	t.Run("returns false when app is not in cancelable state", func(t *testing.T) {
		app := createTestApp("test-app", common.RUNNING, common.PROD)
		assert.False(t, app.IsCancelable())
	})
}

// =============================================================================
// TestTransientToActualState - Tests for state conversion
// =============================================================================

func TestTransientToActualState(t *testing.T) {
	tests := []struct {
		transient common.AppState
		expected  common.AppState
	}{
		{common.BUILDING, common.PRESENT},
		{common.TRANSFERED, common.PRESENT},
		{common.DOWNLOADING, common.PRESENT},
		{common.TRANSFERING, common.PRESENT},
		{common.PUBLISHING, common.PRESENT},
		{common.UPDATING, common.PRESENT},
		{common.STOPPING, common.PRESENT},
		{common.DELETING, common.REMOVED},
		{common.STARTING, common.RUNNING},
		// Stable states should return themselves
		{common.PRESENT, common.PRESENT},
		{common.RUNNING, common.RUNNING},
		{common.REMOVED, common.REMOVED},
		{common.FAILED, common.FAILED},
	}

	for _, tt := range tests {
		t.Run(string(tt.transient), func(t *testing.T) {
			actual := common.TransientToActualState(tt.transient)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

// =============================================================================
// TestContainerStateToAppState - Tests for Docker state mapping
// =============================================================================

func TestContainerStateToAppState(t *testing.T) {
	tests := []struct {
		containerState string
		exitCode       int
		expectedState  common.AppState
		expectError    bool
	}{
		{"running", 0, common.RUNNING, false},
		{"created", 0, common.PRESENT, false},
		{"removing", 0, common.STOPPING, false},
		{"restarting", 0, common.FAILED, false},
		{"exited", 0, common.FAILED, false},
		{"exited", 137, common.FAILED, false},
		{"dead", 0, common.FAILED, false},
		{"paused", 0, "", true},
		{"unknown", 0, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.containerState, func(t *testing.T) {
			state, err := common.ContainerStateToAppState(tt.containerState, tt.exitCode)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedState, state)
			}
		})
	}
}

// =============================================================================
// TestMockContainer - Tests for MockContainer functionality
// =============================================================================

func TestMockContainer(t *testing.T) {
	t.Run("tracks method calls", func(t *testing.T) {
		mock := NewMockContainer()

		mock.GetConfig()
		mock.Ping(nil)
		mock.Pull(nil, "", container.PullOptions{})

		assert.True(t, mock.HasCall("Ping"))
		assert.True(t, mock.HasCall("Pull"))
		assert.False(t, mock.HasCall("Push"))
	})

	t.Run("returns configured errors", func(t *testing.T) {
		mock := NewMockContainer()
		mock.PullError = assert.AnError

		_, err := mock.Pull(nil, "test-image", container.PullOptions{})
		assert.Error(t, err)
	})

	t.Run("reset clears call history", func(t *testing.T) {
		mock := NewMockContainer()
		mock.Ping(nil)
		assert.True(t, mock.HasCall("Ping"))

		mock.ResetCalls()
		assert.False(t, mock.HasCall("Ping"))
	})
}

// =============================================================================
// Placeholder tests for actual transition execution
// These require more setup and will be expanded
// =============================================================================

func TestInitTransition(t *testing.T) {
	t.Skip("TODO: Implement full transition execution tests")

	// Example structure for future tests:
	// sm, mockContainer, _ := setupTestStateMachine()
	// app := createTestApp("test-app", common.REMOVED, common.PROD)
	// payload := createTestPayload("test-app", common.PRESENT, common.PROD)
	//
	// errChan := sm.InitTransition(app, payload)
	// err := <-errChan
	//
	// assert.NoError(t, err)
	// assert.True(t, mockContainer.HasCall("Pull"))
}
