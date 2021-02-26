package messenger

import (
	"context"
	"reagent/common"
	"reagent/config"
	"reagent/messenger/topics"
)

type OfflineMessenger struct {
	config *config.Config
}

func NewOffline(config *config.Config) *OfflineMessenger {
	return &OfflineMessenger{config}
}

func (om OfflineMessenger) Register(topic topics.Topic, cb func(ctx context.Context, invocation Result) (*InvokeResult, error), options common.Dict) error {
	return nil
}

func (om OfflineMessenger) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	return nil
}

func (om OfflineMessenger) Subscribe(topic topics.Topic, cb func(Result) error, options common.Dict) error {
	return nil
}

func (om OfflineMessenger) Call(ctx context.Context, topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict, progCb func(Result)) (Result, error) {
	return Result{}, nil
}

func (om OfflineMessenger) SubscriptionID(topic topics.Topic) (id uint64, ok bool) {
	return 0, true
}

func (om OfflineMessenger) RegistrationID(topic topics.Topic) (id uint64, ok bool) {
	return 0, true
}

func (om OfflineMessenger) Unregister(topic topics.Topic) error {
	return nil
}

func (om OfflineMessenger) Unsubscribe(topic topics.Topic) error {
	return nil
}

func (om OfflineMessenger) SetupTestament() error {
	return nil
}

func (om OfflineMessenger) GetSessionID() uint64 {
	return 0
}

func (om OfflineMessenger) GetConfig() *config.Config {
	return om.config
}

func (om OfflineMessenger) Done() <-chan struct{} {
	return nil
}

func (om OfflineMessenger) Connected() bool {
	return false
}

func (om OfflineMessenger) Reconnect() {

}

func (om OfflineMessenger) Close() {

}
