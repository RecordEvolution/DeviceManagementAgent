package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The package holds global state; reset it between tests.
func reset() {
	mu.Lock()
	requests = nil
	mu.Unlock()
}

func TestRequestRestartWithoutSupervisor(t *testing.T) {
	reset()

	assert.False(t, Supervised())
	err := RequestRestart("update")
	require.ErrorIs(t, err, ErrNotSupervised)
}

func TestRequestRestartDeliversToSubscriber(t *testing.T) {
	reset()

	ch := Subscribe()
	assert.True(t, Supervised())

	require.NoError(t, RequestRestart("update to v1.2.3"))

	select {
	case reason := <-ch:
		assert.Equal(t, "update to v1.2.3", reason)
	default:
		t.Fatal("expected a pending restart request")
	}
}

func TestConcurrentRequestsCoalesce(t *testing.T) {
	reset()

	ch := Subscribe()

	require.NoError(t, RequestRestart("first"))
	// A second request while one is pending must not block or error.
	require.NoError(t, RequestRestart("second"))

	assert.Equal(t, "first", <-ch)
	select {
	case reason := <-ch:
		t.Fatalf("expected coalesced requests, got a second one: %s", reason)
	default:
	}
}

func TestSubscribeIsIdempotent(t *testing.T) {
	reset()

	first := Subscribe()
	second := Subscribe()

	require.NoError(t, RequestRestart("r"))
	assert.Equal(t, "r", <-second)
	_ = first
}
