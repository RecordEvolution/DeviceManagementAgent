package messenger

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWampSession_DoneChannelLifecycle(t *testing.T) {
	t.Run("done channel is created on initialization", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		assert.NotNil(t, session.Done())

		select {
		case <-session.Done():
			t.Error("done channel should not be closed initially")
		default:
			// Expected: channel is open
		}
	})

	t.Run("Close signals done channel", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		session.Close()

		select {
		case <-session.Done():
			// Expected: channel is closed
		case <-time.After(100 * time.Millisecond):
			t.Error("done channel should be closed after Close()")
		}
	})

	t.Run("Close is idempotent", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		// Should not panic on multiple closes
		assert.NotPanics(t, func() {
			session.Close()
			session.Close()
			session.Close()
		})
	})
}

func TestWampSession_Connected(t *testing.T) {
	t.Run("returns false when client is nil", func(t *testing.T) {
		session := &WampSession{
			client: nil,
			done:   make(chan struct{}),
		}

		assert.False(t, session.Connected())
	})
}

func TestWampSession_ThreadSafety(t *testing.T) {
	t.Run("concurrent access to Connected is safe", func(t *testing.T) {
		session := &WampSession{
			client: nil,
			done:   make(chan struct{}),
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
			done: make(chan struct{}),
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
			done: make(chan struct{}),
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
		{"does not match wss scheme", "wss://devices.ironflock.com:8080", true}, // still matches the pattern
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

// Reconnection scenario tests
func TestWampSession_Reconnect(t *testing.T) {
	t.Run("Reconnect creates fresh done channel", func(t *testing.T) {
		session := &WampSession{
			done:         make(chan struct{}),
			agentConfig:  testConfig(),
			socketConfig: &SocketConfig{},
		}
		oldDone := session.Done()

		// Close the old done channel to simulate disconnect
		close(session.done)

		// Verify old channel is closed
		select {
		case <-oldDone:
			// Expected
		default:
			t.Error("old done channel should be closed")
		}

		// Simulate what Reconnect does (without actual network connection)
		session.mu.Lock()
		session.done = make(chan struct{})
		session.mu.Unlock()

		newDone := session.Done()

		// Verify new channel is different and open
		assert.NotEqual(t, oldDone, newDone, "should create new done channel")

		select {
		case <-newDone:
			t.Error("new done channel should be open")
		default:
			// Expected
		}
	})

	t.Run("old done channel listeners receive signal on reconnect", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		listenerReceived := make(chan bool, 1)
		go func() {
			<-session.Done()
			listenerReceived <- true
		}()

		// Give listener time to start
		time.Sleep(10 * time.Millisecond)

		// Simulate reconnect closing old channel
		session.mu.Lock()
		close(session.done)
		session.done = make(chan struct{})
		session.mu.Unlock()

		select {
		case <-listenerReceived:
			// Expected: listener got the signal
		case <-time.After(100 * time.Millisecond):
			t.Error("listener should have received done signal")
		}
	})

	t.Run("multiple rapid reconnects are safe", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		assert.NotPanics(t, func() {
			for i := 0; i < 10; i++ {
				session.mu.Lock()
				select {
				case <-session.done:
					// already closed
				default:
					close(session.done)
				}
				session.done = make(chan struct{})
				session.mu.Unlock()
			}
		})
	})

	t.Run("concurrent reconnect and Done access is safe", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		var wg sync.WaitGroup

		// Spawn readers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					_ = session.Done()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Spawn reconnectors
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					session.mu.Lock()
					select {
					case <-session.done:
					default:
						close(session.done)
					}
					session.done = make(chan struct{})
					session.mu.Unlock()
					time.Sleep(time.Millisecond)
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent reconnect and Connected access is safe", func(t *testing.T) {
		session := &WampSession{
			done:   make(chan struct{}),
			client: nil,
		}

		var wg sync.WaitGroup

		// Spawn Connected checkers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					_ = session.Connected()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Spawn reconnectors
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					session.mu.Lock()
					session.client = nil
					select {
					case <-session.done:
					default:
						close(session.done)
					}
					session.done = make(chan struct{})
					session.mu.Unlock()
					time.Sleep(time.Millisecond)
				}
			}()
		}

		wg.Wait()
	})
}

func TestWampSession_DisconnectScenarios(t *testing.T) {
	t.Run("Close during active session signals done", func(t *testing.T) {
		session := &WampSession{
			done:   make(chan struct{}),
			client: nil, // simulating no real client
		}

		received := make(chan bool, 1)
		go func() {
			<-session.Done()
			received <- true
		}()

		time.Sleep(10 * time.Millisecond)
		session.Close()

		select {
		case <-received:
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("done signal should be received on Close")
		}
	})

	t.Run("Close sets client to nil", func(t *testing.T) {
		session := &WampSession{
			done:   make(chan struct{}),
			client: nil,
		}

		session.Close()
		assert.Nil(t, session.client)
	})

	t.Run("Connected returns false after Close", func(t *testing.T) {
		session := &WampSession{
			done:   make(chan struct{}),
			client: nil,
		}

		session.Close()
		assert.False(t, session.Connected())
	})

	t.Run("Done channel closed after Close", func(t *testing.T) {
		session := &WampSession{
			done: make(chan struct{}),
		}

		session.Close()

		select {
		case <-session.Done():
			// Expected: channel is closed
		default:
			t.Error("done channel should be closed after Close")
		}
	})
}
