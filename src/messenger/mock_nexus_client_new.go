package messenger

import (
	"context"
	"errors"
	"sync"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// =============================================================================
// Error Constants
// =============================================================================

// ErrMockConnectionFailed is returned when MockClient is configured to fail connections.
var ErrMockConnectionFailed = errors.New("mock connection failed")

// ErrMockCallLimitExceeded is returned when MockNexusClient exceeds its call limit.
var ErrMockCallLimitExceeded = errors.New("mock call limit exceeded")

// =============================================================================
// MockClient - Configurable mock that mirrors the real client structure
// =============================================================================

// MockClient is a configurable mock client that produces MockNexusClients.
// It mirrors the structure of the real nexus client where you configure
// connection behavior and then call ConnectNet to get a NexusClient.
//
// Usage:
//
//	mockClient := NewMockClient()
//	mockClient.SetConnectFailCount(2) // Fail first 2 connection attempts
//	session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
//	// After successful connection:
//	nexusClient := mockClient.LastClient()
type MockClient struct {
	mu sync.Mutex

	// Connection behavior configuration
	connectFailCount int   // Number of times ConnectNet should fail before succeeding
	connectError     error // Error to return on connection failure

	// Client configuration (applied to created MockNexusClients)
	clientCallLimit      int   // Max calls before client fails (0 = unlimited)
	clientCallError      error // Error to return on Call operations
	clientPublishError   error // Error to return on Publish operations
	clientDontSignalDone bool  // Whether Close() should NOT signal Done()

	// Custom client configurator for advanced scenarios
	clientConfigurator func(*MockNexusClient)

	// Tracking
	connectAttempts int
	clients         []*MockNexusClient
}

// NewMockClient creates a new MockClient with default settings.
// By default, connections succeed immediately and clients work normally.
func NewMockClient() *MockClient {
	return &MockClient{
		connectError: ErrMockConnectionFailed,
	}
}

// =============================================================================
// Connection Configuration
// =============================================================================

// SetConnectFailCount sets how many connection attempts should fail before succeeding.
// Pass 0 for immediate success (default).
func (m *MockClient) SetConnectFailCount(count int) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectFailCount = count
	return m
}

// SetConnectError sets the error returned on failed connection attempts.
// Defaults to ErrMockConnectionFailed.
func (m *MockClient) SetConnectError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectError = err
	return m
}

// =============================================================================
// Client Configuration (applied to created MockNexusClients)
// =============================================================================

// SetClientCallLimit sets the maximum number of Call operations before
// the created clients start returning ErrMockCallLimitExceeded.
// Pass 0 for unlimited (default).
func (m *MockClient) SetClientCallLimit(limit int) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientCallLimit = limit
	return m
}

// SetClientCallError sets the error that created clients will return on Call operations.
// Pass nil for successful calls (default).
func (m *MockClient) SetClientCallError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientCallError = err
	return m
}

// SetClientPublishError sets the error that created clients will return on Publish operations.
// Pass nil for successful publishes (default).
func (m *MockClient) SetClientPublishError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientPublishError = err
	return m
}

// SetClientDontSignalDone configures whether created clients should NOT signal Done() on Close().
// When true, simulates the nexus bug where ping timeout doesn't properly signal Done().
func (m *MockClient) SetClientDontSignalDone(value bool) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientDontSignalDone = value
	return m
}

// SetClientConfigurator sets a custom function to configure each created MockNexusClient.
// This is called after applying other client settings, allowing advanced customization.
func (m *MockClient) SetClientConfigurator(fn func(*MockNexusClient)) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientConfigurator = fn
	return m
}

// =============================================================================
// ConnectNet - The ClientProvider function
// =============================================================================

// ConnectNet is the ClientProvider function. Pass this to NewWampSession.
//
// Usage:
//
//	mockClient := NewMockClient()
//	session, err := NewWampSession(cfg, socketConfig, nil, mockClient.ConnectNet)
func (m *MockClient) ConnectNet(ctx context.Context, url string, cfg client.Config) (NexusClient, error) {
	m.mu.Lock()
	m.connectAttempts++
	attempt := m.connectAttempts
	failCount := m.connectFailCount
	connectErr := m.connectError

	// Capture client config while holding lock
	callLimit := m.clientCallLimit
	callError := m.clientCallError
	publishError := m.clientPublishError
	dontSignalDone := m.clientDontSignalDone
	configurator := m.clientConfigurator
	m.mu.Unlock()

	// Fail for the configured number of attempts
	if attempt <= failCount {
		return nil, connectErr
	}

	// Create successful client
	nexusClient := &MockNexusClient{
		connected:      true,
		done:           make(chan struct{}),
		callLimit:      callLimit,
		callError:      callError,
		publishError:   publishError,
		dontSignalDone: dontSignalDone,
	}

	// Apply custom configurator if set
	if configurator != nil {
		configurator(nexusClient)
	}

	m.mu.Lock()
	m.clients = append(m.clients, nexusClient)
	m.mu.Unlock()

	return nexusClient, nil
}

// =============================================================================
// Inspection Functions
// =============================================================================

// ConnectAttempts returns the number of ConnectNet calls made.
func (m *MockClient) ConnectAttempts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connectAttempts
}

// Clients returns all MockNexusClients created by ConnectNet.
func (m *MockClient) Clients() []*MockNexusClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*MockNexusClient, len(m.clients))
	copy(result, m.clients)
	return result
}

// LastClient returns the most recently created MockNexusClient, or nil if none.
func (m *MockClient) LastClient() *MockNexusClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.clients) == 0 {
		return nil
	}
	return m.clients[len(m.clients)-1]
}

// ClientCount returns the number of MockNexusClients created.
func (m *MockClient) ClientCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clients)
}

// Reset clears all tracking state (attempts and clients) but keeps configuration.
func (m *MockClient) Reset() *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectAttempts = 0
	m.clients = nil
	return m
}

// =============================================================================
// MockNexusClient - Mock implementation of NexusClient
// =============================================================================

// MockNexusClient is a mock implementation of NexusClient for testing.
// It is created by MockClient.ConnectNet() and can be configured to
// simulate various error conditions.
type MockNexusClient struct {
	mu sync.Mutex

	// Connection state
	connected      bool
	done           chan struct{}
	dontSignalDone bool // When true, Close() won't close the done channel

	// Error configuration
	callError      error
	publishError   error
	registerError  error
	subscribeError error

	// Call limit
	callLimit int // When > 0, fails after this many calls

	// Call tracking
	callCount      int
	publishCount   int
	registerCount  int
	subscribeCount int

	// Custom result for Call
	callResult *wamp.Result

	// Custom call handler for complex test scenarios
	onCall func(procedure string) (*wamp.Result, error)
}

// =============================================================================
// MockNexusClient - Simulation Functions
// =============================================================================

// SimulateConnectionDrop simulates the server dropping the connection.
// This closes the Done() channel and sets Connected() to false.
func (m *MockNexusClient) SimulateConnectionDrop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		m.connected = false
		close(m.done)
	}
}

// SimulateBrokenConnection simulates a connection that appears connected
// but all calls fail. The Done() channel is NOT closed, simulating
// the nexus bug where ping timeout doesn't properly signal Done().
func (m *MockNexusClient) SimulateBrokenConnection(callError error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = true
	m.callError = callError
	m.dontSignalDone = true
}

// SetCallError sets an error to be returned on Call operations.
// Pass nil to clear the error.
func (m *MockNexusClient) SetCallError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callError = err
}

// SetPublishError sets an error to be returned on Publish operations.
// Pass nil to clear the error.
func (m *MockNexusClient) SetPublishError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishError = err
}

// SetRegisterError sets an error to be returned on Register operations.
// Pass nil to clear the error.
func (m *MockNexusClient) SetRegisterError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerError = err
}

// SetSubscribeError sets an error to be returned on Subscribe operations.
// Pass nil to clear the error.
func (m *MockNexusClient) SetSubscribeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribeError = err
}

// SetCallResult sets a custom result to be returned on Call operations.
// Pass nil to return an empty result.
func (m *MockNexusClient) SetCallResult(result *wamp.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callResult = result
}

// SetOnCall sets a custom handler for Call operations.
// This allows complex test scenarios where different calls need different results.
// Pass nil to use the default behavior.
func (m *MockNexusClient) SetOnCall(handler func(procedure string) (*wamp.Result, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCall = handler
}

// SetDontSignalDone configures whether Close() should signal the Done() channel.
// When true, simulates the nexus bug where ping timeout doesn't properly signal Done().
func (m *MockNexusClient) SetDontSignalDone(value bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dontSignalDone = value
}

// SetCallLimit sets the maximum number of calls before returning ErrMockCallLimitExceeded.
// Pass 0 to disable the limit.
func (m *MockNexusClient) SetCallLimit(limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callLimit = limit
}

// =============================================================================
// MockNexusClient - Inspection Functions
// =============================================================================

// CallCount returns the number of Call operations made.
func (m *MockNexusClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// PublishCount returns the number of Publish operations made.
func (m *MockNexusClient) PublishCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publishCount
}

// RegisterCount returns the number of Register operations made.
func (m *MockNexusClient) RegisterCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerCount
}

// SubscribeCount returns the number of Subscribe operations made.
func (m *MockNexusClient) SubscribeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subscribeCount
}

// =============================================================================
// MockNexusClient - NexusClient Interface Implementation
// =============================================================================

func (m *MockNexusClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		m.connected = false
		if !m.dontSignalDone {
			close(m.done)
		}
	}
	return nil
}

func (m *MockNexusClient) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *MockNexusClient) Done() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

func (m *MockNexusClient) ID() wamp.ID {
	return wamp.ID(12345)
}

func (m *MockNexusClient) Publish(topic string, options wamp.Dict, args wamp.List, kwargs wamp.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.publishCount++
	return m.publishError
}

func (m *MockNexusClient) Subscribe(topic string, fn client.EventHandler, options wamp.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.subscribeCount++
	return m.subscribeError
}

func (m *MockNexusClient) SubscriptionID(topic string) (wamp.ID, bool) {
	return wamp.ID(100), true
}

func (m *MockNexusClient) Unsubscribe(topic string) error {
	return nil
}

func (m *MockNexusClient) Call(ctx context.Context, procedure string, options wamp.Dict, args wamp.List, kwargs wamp.Dict, progCb client.ProgressHandler) (*wamp.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	// Check call limit
	if m.callLimit > 0 && m.callCount > m.callLimit {
		return nil, ErrMockCallLimitExceeded
	}

	// Use custom handler if set
	if m.onCall != nil {
		return m.onCall(procedure)
	}

	if m.callError != nil {
		return nil, m.callError
	}

	if m.callResult != nil {
		return m.callResult, nil
	}

	return &wamp.Result{}, nil
}

func (m *MockNexusClient) Register(procedure string, fn client.InvocationHandler, options wamp.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registerCount++
	return m.registerError
}

func (m *MockNexusClient) RegistrationID(procedure string) (wamp.ID, bool) {
	return wamp.ID(200), true
}

func (m *MockNexusClient) Unregister(procedure string) error {
	return nil
}

// Ensure MockNexusClient implements NexusClient
var _ NexusClient = (*MockNexusClient)(nil)
