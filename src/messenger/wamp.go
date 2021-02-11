package messenger

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"reagent/common"
	"reagent/config"
	"reagent/messenger/topics"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type WampSession struct {
	client *client.Client
	config config.Config
}

type wampLogWrapper struct {
	logger *zerolog.Logger
}

func newWampLogger(zeroLogger *zerolog.Logger) wampLogWrapper {
	return wampLogWrapper{logger: zeroLogger}
}

func (wl wampLogWrapper) Print(v ...interface{}) {
	wl.logger.Print(v)
}

func (wl wampLogWrapper) Println(v ...interface{}) {
	wl.logger.Print(v, "\n")
}

func (wl wampLogWrapper) Printf(format string, v ...interface{}) {
	wl.logger.Printf(format, v)
}

func wrapZeroLogger(zeroLogger zerolog.Logger) wampLogWrapper {
	wrapper := newWampLogger(&zeroLogger)
	return wrapper
}

// New creates a new wamp session from a ReswarmConfig file
func NewWamp(config config.Config) (*WampSession, error) {
	ctx := context.Background()
	reswarmConfig := config.ReswarmConfig

	tlscert, err := tls.X509KeyPair([]byte(reswarmConfig.Authentication.Certificate), []byte(reswarmConfig.Authentication.Key))
	if err != nil {
		return nil, err
	}

	cfg := client.Config{
		Realm: "realm1",
		HelloDetails: wamp.Dict{
			"authid": fmt.Sprintf("%d-%d", reswarmConfig.SwarmKey, reswarmConfig.DeviceKey),
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc(reswarmConfig.Secret),
		},
		Debug:           config.CommandLineArguments.DebugMessaging,
		ResponseTimeout: 5 * time.Second,
		Logger:          wrapZeroLogger(log.Logger),
		// Serialization:
		TlsCfg: &tls.Config{
			Certificates:       []tls.Certificate{tlscert},
			InsecureSkipVerify: true,
		},
	}

	// set up WAMP client and connect connect to websocket endpoint
	client, err := client.ConnectNet(ctx, reswarmConfig.DeviceEndpointURL, cfg)
	if err != nil {
		return nil, err
	}

	return &WampSession{client: client, config: config}, nil
}

func (wampSession *WampSession) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	return wampSession.client.Publish(string(topic), wamp.Dict(options), args, wamp.Dict(kwargs))
}

func (wampSession *WampSession) Subscribe(topic topics.Topic, cb func(Result), options common.Dict) error {
	handler := func(event *wamp.Event) {
		cbEventMap := Result{
			Subscription: uint64(event.Subscription),
			Publication:  uint64(event.Publication),
			Details:      common.Dict(event.Details),
			Arguments:    []interface{}(event.Arguments),
			ArgumentsKw:  common.Dict(event.ArgumentsKw),
		}
		cb(cbEventMap)
	}

	return wampSession.client.Subscribe(string(topic), handler, wamp.Dict(options))
}

func (wampSession *WampSession) GetConfig() config.Config {
	return wampSession.config
}

func (wampSession *WampSession) SubscriptionID(topic topics.Topic) (id uint64, ok bool) {
	subID, ok := wampSession.client.SubscriptionID(string(topic))
	return uint64(subID), ok
}
func (wampSession *WampSession) RegistrationID(topic topics.Topic) (id uint64, ok bool) {
	subID, ok := wampSession.client.RegistrationID(string(topic))
	return uint64(subID), ok
}

func (wampSession *WampSession) Call(
	ctx context.Context,
	topic topics.Topic,
	args []interface{},
	kwargs common.Dict,
	options common.Dict,
	progCb func(Result)) (Result, error) {

	handler := func(result *wamp.Result) {
		cbResultMap := Result{
			Request:     uint64(result.Request),
			Details:     common.Dict(result.Details),
			Arguments:   []interface{}(result.Arguments),
			ArgumentsKw: common.Dict(result.ArgumentsKw),
		}
		progCb(cbResultMap)
	}

	result, err := wampSession.client.Call(ctx, string(topic), wamp.Dict(options), args, wamp.Dict(kwargs), handler)

	if err != nil {
		return Result{}, err
	}

	callResultMap := Result{
		Request:     uint64(result.Request),
		Details:     common.Dict(result.Details),
		Arguments:   []interface{}(result.Arguments),
		ArgumentsKw: common.Dict(result.ArgumentsKw),
	}

	return callResultMap, nil
}

func (wampSession *WampSession) Register(topic topics.Topic, cb func(ctx context.Context, invocation Result) InvokeResult, options common.Dict) error {

	invocationHandler := func(ctx context.Context, invocation *wamp.Invocation) client.InvokeResult {
		cbInvocationMap := Result{
			Request:      uint64(invocation.Request),
			Registration: uint64(invocation.Registration),
			Details:      common.Dict(invocation.Details),
			Arguments:    invocation.Arguments,
			ArgumentsKw:  common.Dict(invocation.ArgumentsKw),
		}
		resultMap := cb(ctx, cbInvocationMap)
		kwargs := resultMap.ArgumentsKw
		err := resultMap.Err

		return client.InvokeResult{Args: resultMap.Arguments, Kwargs: wamp.Dict(kwargs), Err: wamp.URI(err)}
	}

	err := wampSession.client.Register(string(topic), invocationHandler, wamp.Dict(options))
	if err != nil {
		return err
	}

	return nil
}

func (wampSession *WampSession) Close() error {
	return wampSession.client.Close()
}

func clientAuthFunc(deviceSecret string) func(c *wamp.Challenge) (string, wamp.Dict) {
	return func(c *wamp.Challenge) (string, wamp.Dict) {
		return crsign.RespondChallenge(deviceSecret, c, nil), wamp.Dict{}
	}
}

// func DeviceHandshake(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
// 	nowis := time.Now().String()
// 	deviceid := "813e9e53-fe1f-4a27-a1bc-a97e8846a5a2"
// 	return client.InvokeResult{Args: wamp.List{nowis, deviceid}}
// }
