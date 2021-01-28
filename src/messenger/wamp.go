package messenger

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"reagent/api/common"
	"reagent/config"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

type WampSession struct {
	client *client.Client
	config config.Config
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
		Debug:           true,
		ResponseTimeout: 5 * time.Second,
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

func (wampSession *WampSession) Publish(topic string, args []common.Dict, kwargs common.Dict, options common.Dict) error {
	var wampList []interface{}
	for _, dict := range args {
		wampList = append(wampList, dict)
	}
	return wampSession.client.Publish(topic, wamp.Dict(options), wampList, wamp.Dict(kwargs))
}

func (wampSession *WampSession) Subscribe(topic string, cb func(Result), options common.Dict) error {
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

	return wampSession.client.Subscribe(topic, handler, wamp.Dict(options))
}

func (wampSession *WampSession) GetConfig() *config.ReswarmConfig {
	return wampSession.config
}

func (wampSession *WampSession) Call(
	ctx context.Context,
	topic string,
	args []common.Dict,
	kwargs common.Dict,
	options common.Dict,
	progCb func(Result)) (Result, error) {

	var wampList []interface{}
	for _, dict := range args {
		wampList = append(wampList, dict)
	}

	handler := func(result *wamp.Result) {
		cbResultMap := Result{
			Request:     uint64(result.Request),
			Details:     common.Dict(result.Details),
			Arguments:   []interface{}(result.Arguments),
			ArgumentsKw: common.Dict(result.ArgumentsKw),
		}
		progCb(cbResultMap)
	}

	result, err := wampSession.client.Call(ctx, topic, wamp.Dict(options), wampList, wamp.Dict(kwargs), handler)

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

func (wampSession *WampSession) Register(topic string, cb func(ctx context.Context, invocation Result) InvokeResult, options common.Dict) error {

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

	err := wampSession.client.Register(topic, invocationHandler, wamp.Dict(options))
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
