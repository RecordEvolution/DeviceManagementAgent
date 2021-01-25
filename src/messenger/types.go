package messenger

import (
	"context"
	"reagent/fs"
)

// Dict is an alias for map[string]interface{}
type Dict map[string]interface{}

// Result the value that is received from a Messenger request
// E.g. Call, Register callback, Subscription callback
type Result struct {
	Arguments    []interface{}
	ArgumentsKw  Dict
	Details      Dict
	Registration uint64 // Values will be filled based on Result type, e.g. Call, Subcribe, Register...
	Request      uint64
	Subscription uint64
	Publication  uint64
}

// InvokeResult the value that is sent whenever a procedure is invoked
type InvokeResult struct {
	Arguments   []interface{}
	ArgumentsKw Dict
	Err         string
}

type Messenger interface {
	Register(topic string, cb func(ctx context.Context, invocation Result) InvokeResult, options Dict) error
	Publish(topic string, args []Dict, kwargs Dict, options Dict) error
	Subscribe(topic string, cb func(Result), options Dict) error
	Call(ctx context.Context, topic string, args []Dict, kwargs Dict, options Dict, progCb func(Result)) (Result, error)
	GetConfig() *fs.ReswarmConfig
	Close() error
}
