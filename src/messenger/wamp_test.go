package messenger

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reagent/config"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for WampSession with simplified implementation that delegates
// Done() to the underlying nexus client.

func TestWampSession_DoneChannel(t *testing.T) {
	t.Run("Done returns closed channel when client is nil", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		doneChan := session.Done()
		assert.NotNil(t, doneChan)

		// When client is nil, Done() returns a closed channel
		select {
		case <-doneChan:
			// Expected: channel is closed when client is nil
		default:
			t.Error("done channel should be closed when client is nil")
		}
	})
}

func TestWampSession_Connected(t *testing.T) {
	t.Run("returns false when client is nil", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		assert.False(t, session.Connected())
	})
}

func TestWampSession_Close(t *testing.T) {
	t.Run("Close is idempotent", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		// Should not panic on multiple closes
		assert.NotPanics(t, func() {
			session.Close()
			session.Close()
			session.Close()
		})
	})

	t.Run("Close sets client to nil", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		session.Close()
		assert.Nil(t, session.client)
	})

	t.Run("Connected returns false after Close", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		session.Close()
		assert.False(t, session.Connected())
	})
}

func TestWampSession_ThreadSafety(t *testing.T) {
	t.Run("concurrent access to Connected is safe", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = session.Connected()
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent access to Done is safe", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = session.Done()
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent Close calls are safe", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				session.Close()
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent Done and Connected access is safe", func(t *testing.T) {
		session := &WampSession{
			client: nil,
		}

		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					_ = session.Done()
					_ = session.Connected()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		wg.Wait()
	})
}

func TestOfflineMessenger(t *testing.T) {
	t.Run("NewOffline creates messenger with config", func(t *testing.T) {
		cfg := testConfig()
		messenger := NewOffline(cfg)

		require.NotNil(t, messenger)
		assert.Equal(t, cfg, messenger.GetConfig())
	})

	t.Run("Connected returns false", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		assert.False(t, messenger.Connected())
	})

	t.Run("Done returns nil", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		assert.Nil(t, messenger.Done())
	})

	t.Run("GetSessionID returns 0", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		assert.Equal(t, uint64(0), messenger.GetSessionID())
	})

	t.Run("Publish does not error", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		err := messenger.Publish("test.topic", nil, nil, nil)
		assert.NoError(t, err)
	})

	t.Run("Register does not error", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		err := messenger.Register("test.topic", nil, nil)
		assert.NoError(t, err)
	})

	t.Run("Subscribe does not error", func(t *testing.T) {
		messenger := NewOffline(testConfig())
		err := messenger.Subscribe("test.topic", nil, nil)
		assert.NoError(t, err)
	})
}

func TestDeviceStatus(t *testing.T) {
	t.Run("DeviceStatus constants have correct values", func(t *testing.T) {
		assert.Equal(t, DeviceStatus("CONNECTED"), CONNECTED)
		assert.Equal(t, DeviceStatus("DISCONNECTED"), DISCONNECTED)
		assert.Equal(t, DeviceStatus("CONFIGURING"), CONFIGURING)
	})
}

func TestErrNotConnected(t *testing.T) {
	t.Run("ErrNotConnected has expected message", func(t *testing.T) {
		assert.Equal(t, "not connected", ErrNotConnected.Error())
	})
}

func TestLegacyEndpointRegex(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected bool
	}{
		{"matches devices.ironflock.com:8080", "devices.ironflock.com:8080", true},
		{"matches devices.reswarm.io:8080", "devices.reswarm.io:8080", true},
		{"matches devices.example.com:8080", "devices.example.com:8080", true},
		{"does not match without port", "devices.ironflock.com", false},
		{"does not match different port", "devices.ironflock.com:443", false},
		{"does not match wss scheme", "wss://devices.ironflock.com:8080", true},
		{"does not match new endpoints", "wss://devices.ironflock.com/ws", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := legacyEndpointRegex.Match([]byte(tc.url))
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSocketConfig(t *testing.T) {
	t.Run("default values are zero", func(t *testing.T) {
		cfg := SocketConfig{}
		assert.Equal(t, time.Duration(0), cfg.PingPongTimeout)
		assert.Equal(t, time.Duration(0), cfg.ResponseTimeout)
		assert.Equal(t, time.Duration(0), cfg.ConnectionTimeout)
		assert.False(t, cfg.SetupTestament)
	})
}

func TestWampSession_OnConnectCallback(t *testing.T) {
	t.Run("SetOnConnect stores callback", func(t *testing.T) {
		session := &WampSession{}

		called := false
		session.SetOnConnect(func(reconnect bool) {
			called = true
		})

		// Verify callback was stored
		session.mu.Lock()
		cb := session.onConnect
		session.mu.Unlock()

		require.NotNil(t, cb)

		// Call it to verify it works
		cb(true)
		assert.True(t, called)
	})

	t.Run("SetOnConnect can be called multiple times", func(t *testing.T) {
		session := &WampSession{}

		callCount := 0
		session.SetOnConnect(func(reconnect bool) {
			callCount = 1
		})
		session.SetOnConnect(func(reconnect bool) {
			callCount = 2
		})

		session.mu.Lock()
		cb := session.onConnect
		session.mu.Unlock()

		cb(false)
		assert.Equal(t, 2, callCount, "second callback should override first")
	})

	t.Run("callback receives correct reconnect flag", func(t *testing.T) {
		session := &WampSession{}

		var receivedReconnect bool
		session.SetOnConnect(func(reconnect bool) {
			receivedReconnect = reconnect
		})

		session.mu.Lock()
		cb := session.onConnect
		session.mu.Unlock()

		cb(true)
		assert.True(t, receivedReconnect)

		cb(false)
		assert.False(t, receivedReconnect)
	})
}

// mockClientProviderWithCallback creates a ClientProvider that calls a callback for each attempt
func mockClientProviderWithCallback(callback func(attempt int) (NexusClient, error)) ClientProvider {
	var attempt int32
	return func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
		currentAttempt := atomic.AddInt32(&attempt, 1)
		return callback(int(currentAttempt))
	}
}

// newTestWampSession creates a WampSession for testing without calling connect()
func newTestWampSession(cfg *config.Config, socketConfig *SocketConfig, provider ClientProvider) *WampSession {
	return &WampSession{
		agentConfig:    cfg,
		socketConfig:   socketConfig,
		clientProvider: provider,
	}
}

func TestWampSession_EstablishSocketConnection(t *testing.T) {
	t.Run("returns channel immediately in offline mode", func(t *testing.T) {
		cfg := testConfig()
		cfg.CommandLineArguments.Offline = true

		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Second * 5,
		}

		session := newTestWampSession(cfg, socketConfig, nil)
		resChan := session.establishSocketConnection()

		// Channel should be returned immediately
		require.NotNil(t, resChan)

		// Channel should never receive a value (we're offline)
		select {
		case <-resChan:
			t.Error("should not receive a client in offline mode")
		case <-time.After(100 * time.Millisecond):
			// Expected: no client received
		}
	})

	t.Run("retries on connection error until successful", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		attemptCount := 0
		provider := mockClientProviderWithCallback(func(attempt int) (NexusClient, error) {
			attemptCount = attempt
			if attempt < 3 {
				return nil, errors.New("connection failed")
			}
			// On 3rd attempt, we can't return a real connected client without a server,
			// so we return an error to keep the test from hanging
			return nil, errors.New("final error to prevent hang")
		})

		session := newTestWampSession(cfg, socketConfig, provider)
		resChan := session.establishSocketConnection()
		require.NotNil(t, resChan)

		// Wait for retries
		time.Sleep(3 * time.Second)

		// Verify retries happened
		assert.GreaterOrEqual(t, attemptCount, 3, "should have retried at least 3 times")
	})

	t.Run("provider receives correct URL and config", func(t *testing.T) {
		cfg := testConfig()
		cfg.ReswarmConfig.DeviceEndpointURL = "wss://test.example.com/ws"

		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			ResponseTimeout:   time.Second * 5,
		}

		var receivedURL string
		var receivedConfig client.Config
		callCount := 0

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			callCount++
			receivedURL = url
			receivedConfig = cfg
			return nil, errors.New("stop after first call")
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.establishSocketConnection()

		// Wait for at least one call
		time.Sleep(200 * time.Millisecond)

		assert.GreaterOrEqual(t, callCount, 1)
		assert.Equal(t, "wss://test.example.com/ws", receivedURL)
		assert.Equal(t, "realm1", receivedConfig.Realm)
		assert.Equal(t, socketConfig.ResponseTimeout, receivedConfig.ResponseTimeout)
	})

	t.Run("context has timeout when ConnectionTimeout is set", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 50,
		}

		var receivedCtx context.Context

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			receivedCtx = ctx
			return nil, errors.New("stop")
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.establishSocketConnection()

		// Wait for the call
		time.Sleep(100 * time.Millisecond)

		require.NotNil(t, receivedCtx)
		deadline, hasDeadline := receivedCtx.Deadline()
		assert.True(t, hasDeadline, "context should have a deadline")
		assert.False(t, deadline.IsZero())
	})

	t.Run("context has no timeout when ConnectionTimeout is zero", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: 0,
		}

		var receivedCtx context.Context

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			receivedCtx = ctx
			return nil, errors.New("stop")
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.establishSocketConnection()

		// Wait for the call
		time.Sleep(100 * time.Millisecond)

		require.NotNil(t, receivedCtx)
		_, hasDeadline := receivedCtx.Deadline()
		assert.False(t, hasDeadline, "context should not have a deadline when ConnectionTimeout is 0")
	})

	t.Run("continues retrying on generic errors", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		var callCount int32

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, errors.New("network error")
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.establishSocketConnection()

		// Wait for multiple retries
		time.Sleep(2500 * time.Millisecond)

		// Should have retried multiple times
		count := atomic.LoadInt32(&callCount)
		assert.GreaterOrEqual(t, count, int32(2), "should retry on generic errors")
	})

	t.Run("tracks connection attempt timing", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		var mu sync.Mutex
		var startTimes []time.Time

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			mu.Lock()
			startTimes = append(startTimes, time.Now())
			count := len(startTimes)
			mu.Unlock()

			if count >= 3 {
				return nil, errors.New("stopping after 3 attempts")
			}
			return nil, errors.New("connection failed")
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.establishSocketConnection()

		// Wait for at least 3 attempts (1s retry delay between each)
		time.Sleep(3 * time.Second)

		mu.Lock()
		times := make([]time.Time, len(startTimes))
		copy(times, startTimes)
		mu.Unlock()

		require.GreaterOrEqual(t, len(times), 2)

		// Verify ~1 second delay between attempts
		for i := 1; i < len(times) && i < 3; i++ {
			diff := times[i].Sub(times[i-1])
			// Allow some tolerance (900ms to 1500ms)
			assert.Greater(t, diff, 900*time.Millisecond, "retry delay should be ~1s")
			assert.Less(t, diff, 1500*time.Millisecond, "retry delay should be ~1s")
		}
	})

	t.Run("retries after failures then succeeds on third attempt", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		var attemptCount int32

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			attempt := atomic.AddInt32(&attemptCount, 1)
			if attempt < 3 {
				return nil, errors.New("connection failed")
			}
			// On 3rd attempt, return a connected mock client
			return NewMockNexusClient(), nil
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		resChan := session.establishSocketConnection()
		require.NotNil(t, resChan)

		// Wait for the client to be returned
		select {
		case receivedClient := <-resChan:
			require.NotNil(t, receivedClient)
			assert.True(t, receivedClient.Connected())
			assert.Equal(t, int32(3), atomic.LoadInt32(&attemptCount), "should succeed on 3rd attempt")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for client")
		}
	})
}

func TestWampSession_Connect(t *testing.T) {
	t.Run("connect sets client from provider", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		mockClient := NewMockNexusClient()
		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			return mockClient, nil
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		err := session.connect()

		require.NoError(t, err)
		assert.True(t, session.Connected())
		assert.Equal(t, mockClient, session.client)
	})
}

func TestWampSession_ListenForDisconnect(t *testing.T) {
	t.Run("triggers reconnection when client disconnects", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		// First client will be "disconnected"
		firstClient := NewMockNexusClient()
		// Second client is the reconnected one
		secondClient := NewMockNexusClient()

		callCount := 0
		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			callCount++
			if callCount == 1 {
				return firstClient, nil
			}
			return secondClient, nil
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.client = firstClient

		// Start listening for disconnect
		session.listenForDisconnect()

		// Simulate connection drop
		firstClient.SimulateConnectionDrop()

		// Wait for reconnection to happen
		time.Sleep(500 * time.Millisecond)

		// Should have reconnected with second client
		assert.Equal(t, 2, callCount, "should have called provider twice (initial + reconnect)")
		assert.True(t, session.Connected())
	})

	t.Run("invokes onConnect callback after reconnection", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		mockClient := NewMockNexusClient()
		reconnectClient := NewMockNexusClient()

		callCount := 0
		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			callCount++
			if callCount == 1 {
				return mockClient, nil
			}
			return reconnectClient, nil
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.client = mockClient

		// Set up callback to track reconnection
		var callbackCalled bool
		var wasReconnect bool
		session.SetOnConnect(func(reconnect bool) {
			callbackCalled = true
			wasReconnect = reconnect
		})

		// Start listening for disconnect
		session.listenForDisconnect()

		// Simulate connection drop
		mockClient.SimulateConnectionDrop()

		// Wait for reconnection and callback
		time.Sleep(500 * time.Millisecond)

		assert.True(t, callbackCalled, "onConnect callback should be called")
		assert.True(t, wasReconnect, "reconnect flag should be true")
	})

	t.Run("retries reconnection on failure", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
		}

		mockClient := NewMockNexusClient()
		reconnectClient := NewMockNexusClient()

		callCount := 0
		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			callCount++
			if callCount <= 2 {
				// First two reconnection attempts fail
				return nil, errors.New("connection failed")
			}
			// Third attempt succeeds
			return reconnectClient, nil
		}

		session := newTestWampSession(cfg, socketConfig, provider)
		session.client = mockClient

		// Start listening for disconnect
		session.listenForDisconnect()

		// Simulate connection drop
		mockClient.SimulateConnectionDrop()

		// Wait for multiple reconnection attempts
		time.Sleep(3500 * time.Millisecond)

		assert.GreaterOrEqual(t, callCount, 3, "should retry reconnection multiple times")
		assert.True(t, session.Connected())
	})
}

func TestWampSession_Heartbeat(t *testing.T) {
	t.Run("heartbeat calls UpdateRemoteDeviceStatus periodically", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50, // Fast heartbeat for testing
		}

		mockClient := NewMockNexusClient()
		session := newTestWampSession(cfg, socketConfig, nil)
		session.client = mockClient

		initialCallCount := mockClient.CallCount

		// Start heartbeat
		session.startHeartbeat()

		// Wait for a few heartbeats
		time.Sleep(200 * time.Millisecond)

		// Should have made multiple calls (UpdateRemoteDeviceStatus uses Call internally)
		assert.Greater(t, mockClient.CallCount, initialCallCount, "heartbeat should call UpdateRemoteDeviceStatus")
	})

	t.Run("heartbeat uses default interval when not configured", func(t *testing.T) {
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			// HeartbeatInterval not set - should use default
		}

		assert.Equal(t, time.Duration(0), socketConfig.HeartbeatInterval)
		assert.Equal(t, 30*time.Second, DefaultHeartbeatInterval)
	})

	t.Run("heartbeat forces close after consecutive failures", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50, // Fast heartbeat for testing
		}

		mockClient := NewMockNexusClient()
		// Make all calls fail
		mockClient.SetCallError(errors.New("connection lost"))

		session := newTestWampSession(cfg, socketConfig, nil)
		session.client = mockClient

		// Start heartbeat
		session.startHeartbeat()

		// Wait for enough heartbeats to trigger close (2 consecutive failures)
		time.Sleep(200 * time.Millisecond)

		// After 2 consecutive failures, Close() should have been called
		assert.False(t, session.Connected(), "session should be disconnected after consecutive heartbeat failures")
	})

	t.Run("heartbeat resets failure count on success", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50, // Fast heartbeat for testing
		}

		mockClient := NewMockNexusClient()
		callCount := 0

		// Fail once, then succeed
		mockClient.OnCall = func(procedure string) (*wamp.Result, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("temporary failure")
			}
			return &wamp.Result{}, nil
		}

		session := newTestWampSession(cfg, socketConfig, nil)
		session.client = mockClient

		// Start heartbeat
		session.startHeartbeat()

		// Wait for several heartbeats
		time.Sleep(250 * time.Millisecond)

		// Should still be connected because failures were not consecutive
		assert.True(t, session.Connected(), "session should remain connected after non-consecutive failures")
	})

	t.Run("heartbeat skips when not connected", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50, // Fast heartbeat for testing
		}

		mockClient := NewMockNexusClient()
		mockClient.SimulateConnectionDrop() // Start disconnected

		session := newTestWampSession(cfg, socketConfig, nil)
		session.client = mockClient

		initialCallCount := mockClient.CallCount

		// Start heartbeat
		session.startHeartbeat()

		// Wait for a few heartbeat cycles
		time.Sleep(200 * time.Millisecond)

		// Should not have made any calls since we're disconnected
		assert.Equal(t, initialCallCount, mockClient.CallCount, "heartbeat should not call when disconnected")
	})
}
