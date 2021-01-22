package messenger

import (
	"context"
)

type Messenger interface {
	Register(topic string, cb func(ctx context.Context, invocation map[string]interface{}) map[string]interface{}, options map[string]interface{}) error
	Publish(topic string, args []map[string]interface{}, kwargs map[string]interface{}, options map[string]interface{}) error
	Subscribe(topic string, cb func(map[string]interface{}), options map[string]interface{}) error
	Call(ctx context.Context, topic string, args []map[string]interface{}, kwargs map[string]interface{}, options map[string]interface{}, progCb func(map[string]interface{})) (map[string]interface{}, error)
	Close() error
}
