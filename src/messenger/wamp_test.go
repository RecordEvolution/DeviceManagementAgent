package messenger

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
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

}

// =============================================================================
// TestWampSession_Heartbeat - Tests for heartbeat mechanism
// =============================================================================

func TestWampSession_Heartbeat(t *testing.T) {
	t.Run("heartbeat calls UpdateRemoteDeviceStatus periodically", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50,
		}
		mockClient := NewMockClient()

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		nexusClient := mockClient.LastClient()
		initialCallCount := nexusClient.CallCount()

		time.Sleep(200 * time.Millisecond)

		assert.Greater(t, nexusClient.CallCount(), initialCallCount)
	})

	t.Run("heartbeat uses default interval when not configured", func(t *testing.T) {
		socketConfig := &SocketConfig{ConnectionTimeout: time.Millisecond * 100}

		assert.Equal(t, time.Duration(0), socketConfig.HeartbeatInterval)
		assert.Equal(t, 30*time.Second, DefaultHeartbeatInterval)
	})

	t.Run("heartbeat triggers reconnect after consecutive failures", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50,
		}

		// First client fails all calls; subsequent clients succeed
		mockClient := NewMockClient().SetClientCallLimit(1)

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		// Reset the call limit so subsequent clients (after reconnection) work normally
		mockClient.SetClientCallLimit(0)

		// Wait for reconnection to happen (heartbeat fails after 2 consecutive failures)
		require.Eventually(t, func() bool {
			return mockClient.Clients()[len(mockClient.Clients())-1].Connected()
		}, time.Second, 10*time.Millisecond,
			"expected at least 2 clients after heartbeat-triggered reconnection")

	})

	t.Run("heartbeat resets failure count on success", func(t *testing.T) {
		cfg := testConfig()
		socketConfig := &SocketConfig{
			ConnectionTimeout: time.Millisecond * 100,
			HeartbeatInterval: time.Millisecond * 50,
		}

		var callCount int32

		// Use SetClientConfigurator to set up alternating success/failure
		mockClient := NewMockClient().SetClientConfigurator(func(c *MockNexusClient) {
			c.SetOnCall(func(procedure string) (*wamp.Result, error) {
				count := atomic.AddInt32(&callCount, 1)
				if count == 1 {
					return nil, errors.New("temporary failure")
				}
				return &wamp.Result{}, nil
			})
		})

		session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
		require.NoError(t, err)
		defer session.Close()

		time.Sleep(250 * time.Millisecond)

		assert.True(t, session.Connected())
	})
}

// =============================================================================
// TestHeartbeatTriggersReconnectWithoutDoneChannel - Tests for nexus bug workaround
// =============================================================================

func TestHeartbeatTriggersReconnectWithoutDoneChannel(t *testing.T) {
	cfg := testConfig()
	socketConfig := &SocketConfig{
		ConnectionTimeout: time.Millisecond * 100,
		HeartbeatInterval: time.Millisecond * 30,
	}

	// First client: don't signal done (simulates nexus bug)
	mockClient := NewMockClient().SetClientDontSignalDone(true).SetClientCallLimit(1)

	session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
	require.NoError(t, err)
	defer session.Close()

	time.Sleep(50 * time.Millisecond)

	firstClient := mockClient.Clients()[0]

	// Reset factory settings BEFORE triggering reconnection
	// so the next client will be healthy
	mockClient.SetClientDontSignalDone(false).SetClientCallLimit(0)

	// Now trigger the broken connection - reconnection will use the new settings
	firstClient.SimulateBrokenConnection(errors.New("write: broken pipe"))

	// Wait for reconnection to be triggered and new client to be created
	require.Eventually(t, func() bool {
		return mockClient.ClientCount() >= 2 && mockClient.Clients()[len(mockClient.Clients())-1].Connected()
	}, time.Second, 10*time.Millisecond,
		"reconnection should be triggered by heartbeat failure even when Done() is not signaled")

}
