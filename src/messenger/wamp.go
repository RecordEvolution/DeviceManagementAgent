package wampsession

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"reagent/fs"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

type WampSession struct {
	client *client.Client
	config *fs.ReswarmConfig
}

// New creates a new wamp session from a ReswarmConfig file
func New(config *fs.ReswarmConfig) WampSession {
	ctx := context.Background()

	tlscert, err := tls.X509KeyPair([]byte(config.Authentication.Certificate), []byte(config.Authentication.Key))
	if err != nil {
		panic(err)
	}

	cfg := client.Config{
		Realm: "realm1",
		HelloDetails: wamp.Dict{
			"authid": fmt.Sprintf("%d-%d", config.SwarmKey, config.DeviceKey),
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc(config.Secret),
		},
		Debug:           true,
		ResponseTimeout: 5 * time.Second,
		// Serialization:
		TlsCfg: &tls.Config{
			Certificates:       []tls.Certificate{tlscert},
			InsecureSkipVerify: true},
	}

	// set up WAMP client and connect connect to websocket endpoint
	client, err := client.ConnectNet(ctx, config.DeviceEndpointURL, cfg)
	if err != nil {
		panic(err)
	}

	return WampSession{client: client, config: config}
}

func (wampSession *WampSession) Publish(topic string, args []map[string]interface{}, kwargs map[string]interface{}, options map[string]interface{}) error {
	var wampList []interface{}
	for _, dict := range args {
		wampList = append(wampList, dict)
	}
	return wampSession.client.Publish(topic, options, wampList, kwargs)
}

func (wampSession *WampSession) Subscribe(topic string, cb func(map[string]interface{}), options map[string]interface{}) error {
	handler := func(event *wamp.Event) {
		cbEventMap := map[string]interface{}{
			"Subscription": event.Subscription,
			"Publication":  event.Publication,
			"Details":      event.Details,
			"Arguments":    event.Arguments,
			"ArgumentsKw":  event.ArgumentsKw,
		}
		cb(cbEventMap)
	}

	return wampSession.client.Subscribe(topic, handler, options)
}

func (wampSession *WampSession) Call(
	ctx context.Context,
	topic string,
	args []map[string]interface{},
	kwargs map[string]interface{},
	options map[string]interface{},
	progCb func(map[string]interface{})) (map[string]interface{}, error) {

	var wampList []interface{}
	for _, dict := range args {
		wampList = append(wampList, dict)
	}

	handler := func(result *wamp.Result) {
		cbResultMap := map[string]interface{}{
			"Request":     result.Request,
			"Details":     result.Details,
			"Arguments":   result.Arguments,
			"ArgumentsKw": result.ArgumentsKw,
		}
		progCb(cbResultMap)
	}

	result, err := wampSession.client.Call(ctx, topic, options, wampList, kwargs, handler)

	if err != nil {
		return nil, err
	}

	callResultMap := map[string]interface{}{
		"Request":     result.Request,
		"Details":     result.Details,
		"Arguments":   result.Arguments,
		"ArgumentsKw": result.ArgumentsKw,
	}

	return callResultMap, nil
}

func (wampSession *WampSession) Register(topic string, cb func(ctx context.Context, invocation map[string]interface{}) map[string]interface{}, options map[string]interface{}) error {

	invocationHandler := func(ctx context.Context, invocation *wamp.Invocation) client.InvokeResult {
		cbInvocationMap := map[string]interface{}{
			"Request":      invocation.Request,
			"Registration": invocation.Registration,
			"Details":      invocation.Details,
			"Arguments":    invocation.Arguments,
			"ArgumentsKw":  invocation.ArgumentsKw,
		}
		resultMap := cb(ctx, cbInvocationMap)
		args := resultMap["Args"].([]interface{})
		kwargs := resultMap["Kwargs"].(map[string]interface{})
		err := resultMap["Err"].(wamp.URI)

		return client.InvokeResult{Args: args, Kwargs: kwargs, Err: err}
	}

	err := wampSession.client.Register(topic, invocationHandler, options)
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
