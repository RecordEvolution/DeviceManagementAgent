package messenger

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

// blockingCall returns an onCall handler that parks until release is closed,
// plus the release func (safe to call more than once).
func blockingCall() (handler func(string) (*wamp.Result, error), release func()) {
	gate := make(chan struct{})
	var once sync.Once
	return func(string) (*wamp.Result, error) {
			<-gate
			return &wamp.Result{}, nil
		}, func() {
			once.Do(func() { close(gate) })
		}
}

// A status push parked behind a predecessor that never returns must give up
// instead of queueing forever; the stall watchdog owns the recovery.
func TestUpdateRemoteDeviceStatus_GivesUpBehindStuckPredecessor(t *testing.T) {
	handler, release := blockingCall()
	t.Cleanup(release)
	mockClient := NewMockClient().SetClientConfigurator(func(m *MockNexusClient) {
		m.SetOnCall(handler)
	})
	session, err := NewWampSession(testConfig(),
		&SocketConfig{ConnectionTimeout: 100 * time.Millisecond, HeartbeatInterval: time.Hour},
		nil, mockClient.ConnectNet)
	require.NoError(t, err)
	t.Cleanup(session.Close)
	session.statusUpdateTimeout = 50 * time.Millisecond

	go func() { _ = session.UpdateRemoteDeviceStatus(CONFIGURING) }() // parks in Call, holding the slot
	time.Sleep(20 * time.Millisecond)

	errc := make(chan error, 1)
	go func() { errc <- session.UpdateRemoteDeviceStatus(CONNECTED) }()
	select {
	case err := <-errc:
		require.ErrorIs(t, err, ErrStatusUpdateStuck)
	case <-time.After(2 * time.Second):
		t.Fatal("second status push queued forever behind a stuck one")
	}
}

// A heartbeat that never completes a round (here: a call that never returns)
// must not leave the device DISCONNECTED for good. The stall watchdog forces
// a fresh connection, and the pushes on that connection go through even
// though the stuck one is still parked.
func TestHeartbeatStall_ForcesReconnectWhosePushesGoThrough(t *testing.T) {
	handler, release := blockingCall()
	t.Cleanup(release)
	var created atomic.Int32
	mockClient := NewMockClient().SetClientConfigurator(func(m *MockNexusClient) {
		if created.Add(1) == 1 {
			m.SetOnCall(handler) // only the first connection is broken
		}
	})
	session, err := NewWampSession(testConfig(),
		&SocketConfig{ConnectionTimeout: 100 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond},
		nil, mockClient.ConnectNet)
	require.NoError(t, err)
	t.Cleanup(session.Close)

	require.Eventually(t, func() bool {
		clients := mockClient.Clients()
		return len(clients) >= 2 && clients[1].CallCount() > 0
	}, 2*time.Second, 10*time.Millisecond, "watchdog never replaced the stalled connection")
	require.Equal(t, 1, mockClient.Clients()[0].CallCount(), "first connection's push must still be parked, not retried")
}
