// Package fakes provides hand-written, stateful in-memory test doubles for
// cases where behavior matters more than call expectations (e.g. simulating a
// messenger session lifecycle). For pure call-assertion mocks, prefer the
// mockery-generated doubles in reagent/testutil/mocks.
package fakes

import (
	"context"
	"sync"

	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/testutil/builders"
)

// Messenger is a configurable, stateful fake implementation of the
// messenger.Messenger interface. It records calls and lets tests configure
// per-topic responses and simulate disconnects.
type Messenger struct {
	mu        sync.RWMutex
	config    *config.Config
	connected bool
	done      chan struct{}
	sessionID uint64

	// Call tracking
	PublishCalls   []PublishCall
	CallCalls      []CallCall
	RegisterCalls  []RegisterCall
	SubscribeCalls []SubscribeCall

	// Configurable responses
	CallResponses map[string]CallResponse
	CallErrors    map[string]error
}

// PublishCall records a Publish call.
type PublishCall struct {
	Topic   topics.Topic
	Args    []interface{}
	Kwargs  common.Dict
	Options common.Dict
}

// CallCall records a Call invocation.
type CallCall struct {
	Topic   topics.Topic
	Args    []interface{}
	Kwargs  common.Dict
	Options common.Dict
}

// RegisterCall records a Register call.
type RegisterCall struct {
	Topic   topics.Topic
	Options common.Dict
}

// SubscribeCall records a Subscribe call.
type SubscribeCall struct {
	Topic   topics.Topic
	Options common.Dict
}

// CallResponse holds a configured response for Call.
type CallResponse struct {
	Result messenger.Result
	Err    error
}

// NewMessenger creates a new fake messenger with default configuration.
func NewMessenger() *Messenger {
	return &Messenger{
		config:        builders.DefaultTestConfig(),
		connected:     true,
		done:          make(chan struct{}),
		sessionID:     12345,
		CallResponses: make(map[string]CallResponse),
		CallErrors:    make(map[string]error),
	}
}

// NewMessengerWithConfig creates a fake messenger with custom config.
func NewMessengerWithConfig(cfg *config.Config) *Messenger {
	m := NewMessenger()
	m.config = cfg
	return m
}

func (m *Messenger) Register(topic topics.Topic, cb func(ctx context.Context, invocation messenger.Result) (*messenger.InvokeResult, error), options common.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RegisterCalls = append(m.RegisterCalls, RegisterCall{Topic: topic, Options: options})
	return nil
}

func (m *Messenger) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PublishCalls = append(m.PublishCalls, PublishCall{
		Topic:   topic,
		Args:    args,
		Kwargs:  kwargs,
		Options: options,
	})
	return nil
}

func (m *Messenger) Subscribe(topic topics.Topic, cb func(messenger.Result) error, options common.Dict) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SubscribeCalls = append(m.SubscribeCalls, SubscribeCall{Topic: topic, Options: options})
	return nil
}

func (m *Messenger) Call(ctx context.Context, topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict, progCb func(messenger.Result)) (messenger.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CallCalls = append(m.CallCalls, CallCall{
		Topic:   topic,
		Args:    args,
		Kwargs:  kwargs,
		Options: options,
	})

	// Check for configured error
	if err, ok := m.CallErrors[string(topic)]; ok {
		return messenger.Result{}, err
	}

	// Check for configured response
	if resp, ok := m.CallResponses[string(topic)]; ok {
		return resp.Result, resp.Err
	}

	return messenger.Result{}, nil
}

func (m *Messenger) SubscriptionID(topic topics.Topic) (id uint64, ok bool) {
	return 0, true
}

func (m *Messenger) RegistrationID(topic topics.Topic) (id uint64, ok bool) {
	return 0, true
}

func (m *Messenger) Unregister(topic topics.Topic) error {
	return nil
}

func (m *Messenger) Unsubscribe(topic topics.Topic) error {
	return nil
}

func (m *Messenger) SetupTestament() error {
	return nil
}

func (m *Messenger) GetSessionID() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

func (m *Messenger) GetConfig() *config.Config {
	return m.config
}

func (m *Messenger) Done() <-chan struct{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.done
}

func (m *Messenger) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *Messenger) SetOnConnect(cb func(reconnect bool)) {
	// No-op: store callback here if a test needs to assert on it.
}

func (m *Messenger) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}

func (m *Messenger) UpdateRemoteDeviceStatus(status messenger.DeviceStatus) error {
	return nil
}

// SetConnected sets the connection state.
func (m *Messenger) SetConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = connected
}

// SimulateDisconnect simulates a connection drop.
func (m *Messenger) SimulateDisconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}

// SetCallResponse configures a response for a specific topic.
func (m *Messenger) SetCallResponse(topic string, result messenger.Result, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallResponses[topic] = CallResponse{Result: result, Err: err}
}

// SetCallError configures an error for a specific topic.
func (m *Messenger) SetCallError(topic string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallErrors[topic] = err
}

// GetPublishCount returns the number of publish calls.
func (m *Messenger) GetPublishCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.PublishCalls)
}

// GetCallCount returns the number of call invocations.
func (m *Messenger) GetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.CallCalls)
}

// Reset clears all recorded calls.
func (m *Messenger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PublishCalls = nil
	m.CallCalls = nil
	m.RegisterCalls = nil
	m.SubscribeCalls = nil
}
