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

func (wampSession *WampSession) Close() error {
	return wampSession.client.Close()
}

func (wampSession *WampSession) GetConfig() *fs.ReswarmConfig {
	return wampSession.config
}

func (wampSession *WampSession) UpdateDevice(ctx context.Context, args wamp.List, kwargs wamp.Dict) (*wamp.Result, error) {
	return wampSession.client.Call(ctx, "reswarm.devices.update_device", nil, args, kwargs, nil)
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
