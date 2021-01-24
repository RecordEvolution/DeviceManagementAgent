package messenger

import (
	"context"
	"reagent/fs"
)

// Dict is an alias for map[string]interface{}
type Dict map[string]interface{}

type InvokeResult struct {
	Args   []Dict
	Kwargs Dict
	Err    string
}

type Messenger interface {
	Register(topic string, cb func(ctx context.Context, invocation Dict) InvokeResult, options Dict) error
	Publish(topic string, args []Dict, kwargs Dict, options Dict) error
	Subscribe(topic string, cb func(Dict), options Dict) error
	Call(ctx context.Context, topic string, args []Dict, kwargs Dict, options Dict, progCb func(Dict)) (Dict, error)
	GetConfig() *fs.ReswarmConfig
	Close() error
}
