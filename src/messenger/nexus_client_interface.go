package messenger

import (
	"context"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// NexusClient defines the interface for the gammazero/nexus WAMP client.
// This interface allows for mocking the nexus client in tests.
type NexusClient interface {
	// Connection management
	Close() error
	Connected() bool
	Done() <-chan struct{}

	// Session info
	ID() wamp.ID

	// Pub/Sub
	Publish(topic string, options wamp.Dict, args wamp.List, kwargs wamp.Dict) error
	Subscribe(topic string, fn client.EventHandler, options wamp.Dict) error
	SubscriptionID(topic string) (subID wamp.ID, ok bool)
	Unsubscribe(topic string) error

	// RPC
	Call(ctx context.Context, procedure string, options wamp.Dict, args wamp.List, kwargs wamp.Dict, progCb client.ProgressHandler) (*wamp.Result, error)
	Register(procedure string, fn client.InvocationHandler, options wamp.Dict) error
	RegistrationID(procedure string) (regID wamp.ID, ok bool)
	Unregister(procedure string) error
}
