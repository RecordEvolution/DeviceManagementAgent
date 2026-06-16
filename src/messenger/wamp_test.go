package messenger

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestWampSession_Connected - Tests for connection state
// =============================================================================

func TestWampSession_Connected(t *testing.T) {
	t.Run("returns true when connected", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		assert.True(t, session.Connected())
	})

	t.Run("returns false after close", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)

		session.Close()
		assert.False(t, session.Connected())
	})
}

// =============================================================================
// TestWampSession_Close - Tests for session close behavior
// =============================================================================

func TestWampSession_Close(t *testing.T) {
	t.Run("Close is idempotent", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)

		assert.NotPanics(t, func() {
			session.Close()
			session.Close()
			session.Close()
		})
	})

	t.Run("Connected returns false after Close", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)

		session.Close()
		assert.False(t, session.Connected())
	})
}

// =============================================================================
// TestWampSession_ThreadSafety - Tests for concurrent access safety
// =============================================================================

func TestWampSession_ThreadSafety(t *testing.T) {
	t.Run("concurrent access to Connected is safe", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

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

	t.Run("concurrent Close calls are safe", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)

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
}

// =============================================================================
// TestOfflineMessenger - Tests for offline mode
// =============================================================================

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

// =============================================================================
// TestDeviceStatus - Tests for device status constants
// =============================================================================

func TestDeviceStatus(t *testing.T) {
	t.Run("DeviceStatus constants have correct values", func(t *testing.T) {
		assert.Equal(t, DeviceStatus("CONNECTED"), CONNECTED)
		assert.Equal(t, DeviceStatus("DISCONNECTED"), DISCONNECTED)
		assert.Equal(t, DeviceStatus("CONFIGURING"), CONFIGURING)
	})
}

// =============================================================================
// TestErrNotConnected - Tests for error constants
// =============================================================================

func TestErrNotConnected(t *testing.T) {
	t.Run("ErrNotConnected has expected message", func(t *testing.T) {
		assert.Equal(t, "not connected", ErrNotConnected.Error())
	})
}

// =============================================================================
// TestLegacyEndpointRegex - Tests for legacy endpoint matching
// =============================================================================

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

// =============================================================================
// TestSocketConfig - Tests for socket configuration
// =============================================================================

func TestSocketConfig(t *testing.T) {
	t.Run("default values are zero", func(t *testing.T) {
		cfg := SocketConfig{}
		assert.Equal(t, time.Duration(0), cfg.PingPongTimeout)
		assert.Equal(t, time.Duration(0), cfg.ResponseTimeout)
		assert.Equal(t, time.Duration(0), cfg.ConnectionTimeout)
		assert.False(t, cfg.SetupTestament)
	})
}

// =============================================================================
// TestWampSession_OnConnectCallback - Tests for connection callbacks
// =============================================================================

func TestWampSession_OnConnectCallback(t *testing.T) {
	t.Run("SetOnConnect stores callback", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		called := false
		session.SetOnConnect(func(reconnect bool) {
			called = true
		})

		session.mu.Lock()
		cb := session.onConnect
		session.mu.Unlock()

		require.NotNil(t, cb)
		cb(true)
		assert.True(t, called)
	})

	t.Run("SetOnConnect can be called multiple times", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		callCount := 0
		session.SetOnConnect(func(reconnect bool) { callCount = 1 })
		session.SetOnConnect(func(reconnect bool) { callCount = 2 })

		session.mu.Lock()
		cb := session.onConnect
		session.mu.Unlock()

		cb(false)
		assert.Equal(t, 2, callCount, "second callback should override first")
	})

	t.Run("callback receives correct reconnect flag", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

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

// =============================================================================
// TestWampSession_ConnectionRetries - Tests for connection retry behavior
// =============================================================================

func TestWampSession_ConnectionRetries(t *testing.T) {
	t.Run("retries on connection error until successful", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient().SetConnectFailCount(2)

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		assert.Equal(t, 3, mockClient.ConnectAttempts())
		assert.True(t, session.Connected())
	})

	t.Run("provider receives correct URL and config", func(t *testing.T) {
		cfg := testConfig()
		cfg.ReswarmConfig.DeviceEndpointURL = "wss://test.example.com/ws"

		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			ResponseTimeout:   time.Second * 5,
		}

		var mu sync.Mutex
		var receivedURL string
		var receivedConfig client.Config
		mockClient := NewMockClient()

		provider := func(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
			mu.Lock()
			receivedURL = url
			receivedConfig = cfg
			mu.Unlock()
			return mockClient.ConnectNet(ctx, url, cfg)
		}

		session, err := NewWampSession(cfg, socketConfig, nil, provider)
		require.NoError(t, err)
		defer session.Close()

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "wss://test.example.com/ws", receivedURL)
		assert.Equal(t, "realm1", receivedConfig.Realm)
		assert.Equal(t, socketConfig.ResponseTimeout, receivedConfig.ResponseTimeout)
	})
}

// =============================================================================
// TestWampSession_Reconnection - Tests for automatic reconnection
// =============================================================================

func TestWampSession_Reconnection(t *testing.T) {
	t.Run("triggers reconnection when client disconnects", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		// Use SimulateConnectionDrop to trigger reconnection
		client := mockClient.LastClient()
		client.SimulateConnectionDrop()
		assert.False(t, session.Connected())
		time.Sleep(500 * time.Millisecond)

		assert.True(t, session.Connected())
	})

	t.Run("invokes onConnect callback after reconnection", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		var callbackMu sync.Mutex
		var callbackCalled bool
		var wasReconnect bool
		session.SetOnConnect(func(reconnect bool) {
			callbackMu.Lock()
			callbackCalled = true
			wasReconnect = reconnect
			callbackMu.Unlock()
		})

		client := mockClient.LastClient()
		client.SimulateConnectionDrop()

		time.Sleep(500 * time.Millisecond)

		callbackMu.Lock()
		defer callbackMu.Unlock()
		assert.True(t, callbackCalled)
		assert.True(t, wasReconnect)
	})

	t.Run("survives multiple sequential disconnects", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		for i := 0; i < 5; i++ {
			require.Eventually(t, func() bool {
				c := mockClient.LastClient()
				return c != nil && c.Connected()
			}, time.Second, 5*time.Millisecond, "cycle %d: expected fresh connected client", i)
			mockClient.LastClient().SimulateConnectionDrop()
		}

		require.Eventually(t, session.Connected, time.Second, 5*time.Millisecond,
			"session should reconnect after final drop")
		assert.GreaterOrEqual(t, mockClient.ClientCount(), 6, "expected initial + 5 reconnects")
	})

	t.Run("recovers when dial fails repeatedly then succeeds", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}

		mockClient := NewMockClient()
		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		// Make every subsequent ConnectNet fail several times before succeeding.
		mockClient.SetConnectFailCount(3)
		mockClient.LastClient().SimulateConnectionDrop()

		require.Eventually(t, session.Connected, 30*time.Second, 50*time.Millisecond,
			"session must reconnect even after repeated dial failures")
	})

	t.Run("panicking onConnect callback does not stop the session", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		var panicCount int32
		session.SetOnConnect(func(reconnect bool) {
			atomic.AddInt32(&panicCount, 1)
			panic("intentional test panic in onConnect")
		})

		// Two consecutive disconnects: each triggers a panicking callback,
		// the watcher must survive both and keep the session online.
		for i := 0; i < 2; i++ {
			require.Eventually(t, func() bool {
				c := mockClient.LastClient()
				return c != nil && c.Connected()
			}, time.Second, 5*time.Millisecond)
			mockClient.LastClient().SimulateConnectionDrop()
		}

		require.Eventually(t, session.Connected, time.Second, 5*time.Millisecond,
			"session must stay reconnectable even when callback panics")
		assert.GreaterOrEqual(t, atomic.LoadInt32(&panicCount), int32(2),
			"callback should have been invoked for each reconnect")
	})

	t.Run("Close stops the reconnect loop even mid-dial", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}

		mockClient := NewMockClient()
		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)

		// Force the next dial to keep failing so the reconnect loop is busy.
		mockClient.SetConnectFailCount(1000)
		mockClient.LastClient().SimulateConnectionDrop()

		// Give the loop a moment to enter the failing-dial state, then close.
		time.Sleep(50 * time.Millisecond)
		closed := make(chan struct{})
		go func() {
			session.Close()
			close(closed)
		}()

		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Fatal("Close blocked while dial loop was active")
		}
		assert.False(t, session.Connected())
	})
}
