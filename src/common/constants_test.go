package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCancelableState(t *testing.T) {
	cancelable := []AppState{BUILDING, PUBLISHING, DOWNLOADING, TRANSFERING, UPDATING}
	notCancelable := []AppState{
		PRESENT, REMOVED, UNINSTALLED, FAILED, BUILT, TRANSFERED,
		PUBLISHED, STARTING, STOPPING, STOPPED, DELETING, RUNNING,
	}

	for _, s := range cancelable {
		t.Run("cancelable_"+string(s), func(t *testing.T) {
			assert.True(t, IsCancelableState(s))
		})
	}
	for _, s := range notCancelable {
		t.Run("not_cancelable_"+string(s), func(t *testing.T) {
			assert.False(t, IsCancelableState(s))
		})
	}
}

func TestIsTransientState(t *testing.T) {
	transient := []AppState{
		BUILDING, BUILT, TRANSFERED, DELETING, TRANSFERING,
		PUBLISHING, PUBLISHED, DOWNLOADING, STOPPING, UPDATING, STARTING,
	}
	notTransient := []AppState{
		PRESENT, REMOVED, UNINSTALLED, FAILED, STOPPED, RUNNING,
	}

	for _, s := range transient {
		t.Run("transient_"+string(s), func(t *testing.T) {
			assert.True(t, IsTransientState(s))
		})
	}
	for _, s := range notTransient {
		t.Run("not_transient_"+string(s), func(t *testing.T) {
			assert.False(t, IsTransientState(s))
		})
	}
}

func TestTransientToActualState(t *testing.T) {
	tests := []struct {
		in       AppState
		expected AppState
	}{
		{BUILDING, PRESENT},
		{TRANSFERED, PRESENT},
		{DOWNLOADING, PRESENT},
		{TRANSFERING, PRESENT},
		{PUBLISHING, PRESENT},
		{UPDATING, PRESENT},
		{STOPPING, PRESENT},
		{DELETING, REMOVED},
		{STARTING, RUNNING},
		// states not in the mapping are returned unchanged
		{RUNNING, RUNNING},
		{PRESENT, PRESENT},
		{FAILED, FAILED},
		{STOPPED, STOPPED},
		{BUILT, BUILT}, // transient but not remapped
		{PUBLISHED, PUBLISHED},
	}
	for _, tt := range tests {
		t.Run(string(tt.in), func(t *testing.T) {
			assert.Equal(t, tt.expected, TransientToActualState(tt.in))
		})
	}
}

func TestContainerStateToAppState(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		exitCode  int
		expected  AppState
		expectErr bool
	}{
		{"running", "running", 0, RUNNING, false},
		{"created", "created", 0, PRESENT, false},
		{"removing", "removing", 0, STOPPING, false},
		{"restarting", "restarting", 0, FAILED, false},
		{"exited zero -> failed", "exited", 0, FAILED, false},
		{"exited nonzero -> failed", "exited", 1, FAILED, false},
		{"dead", "dead", 0, FAILED, false},
		{"paused -> error", "paused", 0, "", true},
		{"unknown -> error", "something-else", 0, "", true},
		{"empty -> error", "", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ContainerStateToAppState(tt.state, tt.exitCode)
			if tt.expectErr {
				require.Error(t, err)
				assert.Equal(t, AppState(""), got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGetCurrentLogType(t *testing.T) {
	tests := []struct {
		state    AppState
		expected LogType
	}{
		{DOWNLOADING, PULL},
		{UPDATING, PULL},
		{PUBLISHING, PUSH},
		{BUILDING, BUILD},
		{RUNNING, APP},
		{PRESENT, APP},
		{FAILED, APP},
		{STARTING, APP},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, GetCurrentLogType(tt.state))
		})
	}
}
