package messenger

import (
	"context"
	"sync"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// MockNexusClient is a mock implementation of NexusClient for testing.
// It allows simulating various connection states and failures.
type MockNexusClient struct {
	mu sync.Mutex

	// Connection state
	connected  bool
	done       chan struct{}
	closeError error

	// Call tracking
	CallCount      int
	CallError      error
	CallResult     *wamp.Result
	PublishCount   int
	PublishError   error
	RegisterCount  int
	RegisterError  error
	SubscribeCount int
	SubscribeError error

	// Callbacks for custom behavior
	OnCall    func(procedure string) (*wamp.Result, error)
	OnPublish func(topic string) error
}

// NewMockNexusClient creates a new mock client in connected state
func NewMockNexusClient() *MockNexusClient {
	return &MockNexusClient{
		connected: true,
		done:      make(chan struct{}),
	}
}

// NewMockNexusClientDisconnected creates a new mock client in disconnected state
func NewMockNexusClientDisconnected() *MockNexusClient {
	done := make(chan struct{})
	close(done) // Already closed = disconnected
	return &MockNexusClient{
		connected: false,
		done:      done,
	}
}

// SimulateConnectionDrop simulates the server dropping the connection
// by closing the done channel and setting connected to false
func (m *MockNexusClient) SimulateConnectionDrop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		m.connected = false
		close(m.done)
	}
}

// SimulateReconnect simulates a successful reconnection
func (m *MockNexusClient) SimulateReconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = true
	m.done = make(chan struct{})
}

// SetCallError sets an error to be returned on next Call
func (m *MockNexusClient) SetCallError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallError = err
}

// SetCallResult sets the result to be returned on next Call
func (m *MockNexusClient) SetCallResult(result *wamp.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallResult = result
}

// NexusClient interface implementation

func (m *MockNexusClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		m.connected = false
		close(m.done)
	}
	return m.closeError
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

	m.PublishCount++

	if m.OnPublish != nil {
		return m.OnPublish(topic)
	}
	return m.PublishError
}

func (m *MockNexusClient) Subscribe(topic string, fn client.EventHandler, options wamp.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SubscribeCount++
	return m.SubscribeError
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

	m.CallCount++

	if m.OnCall != nil {
		return m.OnCall(procedure)
	}

	if m.CallError != nil {
		return nil, m.CallError
	}

	if m.CallResult != nil {
		return m.CallResult, nil
	}

	return &wamp.Result{}, nil
}

func (m *MockNexusClient) Register(procedure string, fn client.InvocationHandler, options wamp.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RegisterCount++
	return m.RegisterError
}

func (m *MockNexusClient) RegistrationID(procedure string) (wamp.ID, bool) {
	return wamp.ID(200), true
}

func (m *MockNexusClient) Unregister(procedure string) error {
	return nil
}

// Ensure MockNexusClient implements NexusClient
var _ NexusClient = (*MockNexusClient)(nil)
