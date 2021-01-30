package messenger

import (
	"context"
	"reagent/common"
	"reagent/config"
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
	Register(topic string, cb func(ctx context.Context, invocation Result) InvokeResult, options common.Dict) error
	Publish(topic string, args []common.Dict, kwargs common.Dict, options common.Dict) error
	Subscribe(topic string, cb func(Result), options common.Dict) error
	Call(ctx context.Context, topic string, args []common.Dict, kwargs common.Dict, options common.Dict, progCb func(Result)) (Result, error)
	GetConfig() config.Config
	Close() error
}
