package messenger

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bounds returns the window a jittered delay for the given base must fall in.
func bounds(base time.Duration) (time.Duration, time.Duration) {
	spread := time.Duration(float64(base) * reconnectBackoffJitter)
	return base - spread, base + spread
}

func TestDialBackoffDoublesUpToTheCap(t *testing.T) {
	expected := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		reconnectBackoffMax,
		reconnectBackoffMax,
		reconnectBackoffMax,
	}

	for i, base := range expected {
		failures := i + 1
		low, high := bounds(base)

		// Sample repeatedly: the jitter must not push any draw outside the
		// window for this step.
		for draw := 0; draw < 200; draw++ {
			got := dialBackoff(failures)
			assert.GreaterOrEqual(t, got, low, "failure #%d drew %s, below %s", failures, got, low)
			assert.LessOrEqual(t, got, high, "failure #%d drew %s, above %s", failures, got, high)
		}
	}
}

// The whole point of the change: a long outage must not be hammered at the
// old ~27 attempts a minute.
func TestDialBackoffThrottlesASustainedOutage(t *testing.T) {
	var elapsed time.Duration
	attempts := 0

	for elapsed < 10*time.Minute {
		attempts++
		elapsed += dialBackoff(attempts)
	}

	assert.Less(t, attempts, 45,
		"a ten-minute outage should cost well under 45 dials; the fixed one-second delay cost about 270")
	assert.Greater(t, attempts, 20, "but the agent must keep trying, not go silent")
}

// A device must come back promptly once the path clears, which is what caps
// the growth.
func TestDialBackoffRecoversWithinTheCap(t *testing.T) {
	worst := dialBackoff(1000)
	_, high := bounds(reconnectBackoffMax)

	assert.LessOrEqual(t, worst, high)
	assert.LessOrEqual(t, high, 25*time.Second,
		"an offline device must not wait longer than this after the network returns")
}

func TestDialBackoffIsAlwaysPositive(t *testing.T) {
	for _, failures := range []int{-5, 0, 1, 2, 31, 63, 1 << 20} {
		got := dialBackoff(failures)
		require.Greater(t, got, time.Duration(0), "failures=%d produced %s", failures, got)
		require.LessOrEqual(t, got, 25*time.Second, "failures=%d produced %s", failures, got)
	}
}

// Without jitter every device on an appliance reconnects in lockstep after a
// router restart, which is its own outage.
func TestDialBackoffIsJittered(t *testing.T) {
	seen := make(map[time.Duration]struct{})
	for i := 0; i < 50; i++ {
		seen[dialBackoff(6)] = struct{}{}
	}

	assert.Greater(t, len(seen), 10,
		"delays at the cap must be spread, not identical across devices")
}

// The non-dial retry paths keep the short fixed delay that
// maxDuplicateSerialAttempts is calibrated against.
func TestDuplicateSerialToleranceWindowStaysShort(t *testing.T) {
	window := time.Duration(maxDuplicateSerialAttempts) * reconnectBackoff

	assert.LessOrEqual(t, window, 15*time.Second,
		"the router must be given seconds to reap a stale session, not minutes")
}

// Wiring: the dial loop must actually use the growing delay, and must reset
// the ramp once the transport comes up. Runs against the fake client, so the
// only real time spent is the backoff itself.
func TestDialAppliesGrowingBackoffBetweenAttempts(t *testing.T) {
	mock := NewMockClient().SetConnectFailCount(3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := &WampSession{
		agentConfig:    testConfig(),
		socketConfig:   &SocketConfig{},
		ctx:            ctx,
		cancel:         cancel,
		clientProvider: mock.ConnectNet,
	}

	start := time.Now()
	nexusClient, err := session.dial()
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, nexusClient)
	assert.Equal(t, 4, mock.ConnectAttempts(), "three failures then one success")

	// Three failures wait 1s, 2s and 4s, so at least 5.6s once the -20%
	// jitter is applied. The old fixed one-second delay finished in about 3s.
	assert.Greater(t, elapsed, 4500*time.Millisecond,
		"the delay between attempts must grow, not stay at one second")
	assert.Less(t, elapsed, 12*time.Second, "but must stay bounded")
}
