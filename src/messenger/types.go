package messenger

import (
	"context"
	"reagent/common"
	"reagent/config"
	"reagent/messenger/topics"
)

// Result the value that is received from a Messenger request
// E.g. Call, Register callback, Subscription callback
type Result struct {
	Arguments    []interface{}
	ArgumentsKw  common.Dict
	Details      common.Dict
	Registration uint64 // Values will be filled based on Result type, e.g. Call, Subcribe, Register...
	Request      uint64
	Subscription uint64
	Publication  uint64
}

// InvokeResult the value that is sent whenever a procedure is invoked
type InvokeResult struct {
	Arguments   []interface{}
	ArgumentsKw common.Dict
	Err         string
}

type Messenger interface {
	Register(topic topics.Topic, cb func(ctx context.Context, invocation Result) InvokeResult, options common.Dict) error
	Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error
	Subscribe(topic topics.Topic, cb func(Result), options common.Dict) error
	Call(ctx context.Context, topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict, progCb func(Result)) (Result, error)
	SubscriptionID(topic topics.Topic) (id uint64, ok bool)
	RegistrationID(topic topics.Topic) (id uint64, ok bool)
	Unregister(topic topics.Topic) error
	Unsubscribe(topic topics.Topic) error
	GetSessionID() uint64
	GetConfig() *config.Config
	Close() error
}
