package messenger

import (
	"context"
	"reagent/common"
	"reagent/config"
	"reagent/messenger/topics"
)

// Result is an alias to common.Result for backward compatibility
type Result = common.Result

// InvokeResult is an alias to common.InvokeResult for backward compatibility
type InvokeResult = common.InvokeResult

type Messenger interface {
	Register(topic topics.Topic, cb func(ctx context.Context, invocation Result) (*InvokeResult, error), options common.Dict) error
	Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error
	Subscribe(topic topics.Topic, cb func(Result) error, options common.Dict) error
	Call(ctx context.Context, topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict, progCb func(Result)) (Result, error)
	SubscriptionID(topic topics.Topic) (id uint64, ok bool)
	RegistrationID(topic topics.Topic) (id uint64, ok bool)
	Unregister(topic topics.Topic) error
	Unsubscribe(topic topics.Topic) error
	SetupTestament() error
	GetSessionID() uint64
	GetConfig() *config.Config
	Connected() bool
	UpdateRemoteDeviceStatus(status DeviceStatus) error
	SetOnConnect(cb func(reconnect bool))
	Close()
}
