package common

import (
	"testing"

	"reagent/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func TestAppSecureTransition(t *testing.T) {
	t.Run("nil semaphore returns false", func(t *testing.T) {
		app := &App{TransitionLock: nil}
		// no semaphore initialized -> cannot secure, returns false
		assert.False(t, app.SecureTransition())
	})

	t.Run("acquires lock on first call, blocked on second", func(t *testing.T) {
		app := &App{TransitionLock: semaphore.NewWeighted(1)}

		// First call: TryAcquire succeeds, so SecureTransition returns !true == false
		// (i.e. the transition was NOT already locked; we just locked it).
		assert.False(t, app.SecureTransition())

		// Second call: TryAcquire fails (already held), so it returns !false == true
		// (the transition is secured/locked).
		assert.True(t, app.SecureTransition())
	})

	t.Run("unlock allows re-acquire", func(t *testing.T) {
		app := &App{TransitionLock: semaphore.NewWeighted(1)}

		require.False(t, app.SecureTransition()) // acquire
		assert.True(t, app.SecureTransition())   // already held

		app.UnlockTransition()

		// after release, acquiring again succeeds -> false
		assert.False(t, app.SecureTransition())
	})
}

func TestAppIsCancelable(t *testing.T) {
	tests := []struct {
		state    AppState
		expected bool
	}{
		{BUILDING, true},
		{PUBLISHING, true},
		{DOWNLOADING, true},
		{TRANSFERING, true},
		{UPDATING, true},
		{RUNNING, false},
		{PRESENT, false},
		{FAILED, false},
		{STOPPED, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			app := &App{CurrentState: tt.state}
			assert.Equal(t, tt.expected, app.IsCancelable())
		})
	}
}

func TestBuildTransitionPayload(t *testing.T) {
	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL:    "registry.example.com/",
			DockerMainRepository: "main/",
		},
	}

	payload := BuildTransitionPayload(
		6,           // appKey
		"MyApp",     // appName
		99,          // requestorAccountKey
		PROD,        // stage
		PRESENT,     // currentState
		RUNNING,     // requestedState
		11,          // releaseKey
		12,          // newReleaseKey
		cfg,
	)

	// Top-level fields copied straight through.
	assert.Equal(t, PROD, payload.Stage)
	assert.Equal(t, RUNNING, payload.RequestedState)
	assert.Equal(t, PRESENT, payload.CurrentState)
	assert.Equal(t, "MyApp", payload.AppName)
	assert.Equal(t, uint64(6), payload.AppKey)
	assert.Equal(t, uint64(11), payload.ReleaseKey)
	assert.Equal(t, uint64(12), payload.NewReleaseKey)
	assert.Equal(t, uint64(99), payload.RequestorAccountKey)

	// initContainerData derived fields (names are lowercased by the builders).
	assert.Equal(t, "pub_6_myapp", payload.PublishContainerName)
	assert.Equal(t, "dev_6_myapp", payload.ContainerName.Dev)
	assert.Equal(t, "prod_6_myapp", payload.ContainerName.Prod)

	// Image names embed the appKey and lowercased app name; arch portion is
	// host-dependent, so only assert the stable prefix/suffix.
	assert.Contains(t, payload.ImageName.Dev, "dev_")
	assert.Contains(t, payload.ImageName.Dev, "_6_myapp")
	assert.Contains(t, payload.ImageName.Prod, "prod_")
	assert.Contains(t, payload.ImageName.Prod, "_6_myapp")

	// Registry image names are prefixed with the configured registry+repo.
	assert.Equal(t, "registry.example.com/main/"+payload.ImageName.Dev, payload.RegistryImageName.Dev)
	assert.Equal(t, "registry.example.com/main/"+payload.ImageName.Prod, payload.RegistryImageName.Prod)
}
