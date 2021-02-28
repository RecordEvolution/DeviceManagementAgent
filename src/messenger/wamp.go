package messenger

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"reagent/common"
	"reagent/config"
	"reagent/messenger/topics"
	"reagent/safe"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type WampSession struct {
	client *client.Client
	config *config.Config
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

var ErrNotConnected = errors.New("not connected")

func createConnectConfig(config *config.Config) (*client.Config, error) {
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
		ResponseTimeout: 3 * time.Second,
		Logger:          wrapZeroLogger(log.Logger),
		TlsCfg: &tls.Config{
			Certificates:       []tls.Certificate{tlscert},
			InsecureSkipVerify: true,
		},
		WsCfg: transport.WebsocketConfig{
			KeepAlive: time.Second * 4,
		},
	}

	return &cfg, nil
}

// New creates a new wamp session from a ReswarmConfig file
func NewWamp(config *config.Config) (*WampSession, error) {
	session := &WampSession{config: config}
	clientChannel := EstablishSocketConnection(config)

	select {
	case client := <-clientChannel:
		session.client = client
	}

	return session, nil
}

func (wampSession *WampSession) Reconnect() {
	wampSession.Close()
	clientChannel := EstablishSocketConnection(wampSession.config)
	select {
	case client := <-clientChannel:
		wampSession.client = client
	}
}

func (wampSession *WampSession) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	return wampSession.client.Publish(string(topic), wamp.Dict(options), args, wamp.Dict(kwargs))
}

func EstablishSocketConnection(config *config.Config) chan *client.Client {
	resChan := make(chan *client.Client)

	// never returns a established connection
	if config.CommandLineArguments.Offline {
		log.Warn().Msg("Started in offline mode, will not establish a socket connection!")
		return resChan
	}

	log.Debug().Msg("Attempting to establish a socket connection...")

	safe.Go(func() {
		for {
			connectionConfig, err := createConnectConfig(config)
			requestStart := time.Now() // time request

			ctx, cancelFunc := context.WithTimeout(context.Background(), time.Millisecond*1250)
			client, err := client.ConnectNet(ctx, config.ReswarmConfig.DeviceEndpointURL, *connectionConfig)

			var duration time.Duration

			if err != nil {
				cancelFunc()
				duration = time.Since(requestStart)
				log.Debug().Stack().Err(err).Msgf("Failed to establish a websocket connection (duration: %s), reattempting... in 100ms", duration.String())
				time.Sleep(time.Millisecond * 100)
				continue
			}

			cancelFunc()
			if client.Connected() {
				duration = time.Since(requestStart)
				log.Debug().Msgf("Sucessfully established a connection (duration: %s)", duration.String())
				resChan <- client
				close(resChan)
				return
			}

			duration = time.Since(requestStart)
			if client != nil {
				client.Close()
			}
			log.Debug().Msgf("A Session was established, but we are not connected (duration: %s)", duration.String())

		}
	})

	return resChan
}

func (wampSession *WampSession) Connected() bool {
	if wampSession.client == nil {
		return false
	}
	return wampSession.client.Connected()
}
func (wampSession *WampSession) Done() <-chan struct{} {
	return wampSession.client.Done()
}

func (wampSession *WampSession) Subscribe(topic topics.Topic, cb func(Result) error, options common.Dict) error {
	handler := func(event *wamp.Event) {
		cbEventMap := Result{
			Subscription: uint64(event.Subscription),
			Publication:  uint64(event.Publication),
			Details:      common.Dict(event.Details),
			Arguments:    []interface{}(event.Arguments),
			ArgumentsKw:  common.Dict(event.ArgumentsKw),
		}
		err := cb(cbEventMap)
		if err != nil {
			log.Error().Stack().Err(err).Msgf("An error occured during the subscribe result of %s", topic)
		}
	}

	if !wampSession.Connected() {
		return ErrNotConnected
	}

	return wampSession.client.Subscribe(string(topic), handler, wamp.Dict(options))
}

func (wampSession *WampSession) GetConfig() *config.Config {
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

	var handler func(result *wamp.Result)

	if progCb != nil {
		handler = func(result *wamp.Result) {
			cbResultMap := Result{
				Request:     uint64(result.Request),
				Details:     common.Dict(result.Details),
				Arguments:   []interface{}(result.Arguments),
				ArgumentsKw: common.Dict(result.ArgumentsKw),
			}
			progCb(cbResultMap)
		}
	}

	if !wampSession.Connected() {
		return Result{}, ErrNotConnected
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

func (wampSession *WampSession) GetSessionID() uint64 {
	if !wampSession.Connected() {
		return 0
	}

	return uint64(wampSession.client.ID())
}

func (wampSession *WampSession) Register(topic topics.Topic, cb func(ctx context.Context, invocation Result) (*InvokeResult, error), options common.Dict) error {

	invocationHandler := func(ctx context.Context, invocation *wamp.Invocation) client.InvokeResult {
		cbInvocationMap := Result{
			Request:      uint64(invocation.Request),
			Registration: uint64(invocation.Registration),
			Details:      common.Dict(invocation.Details),
			Arguments:    invocation.Arguments,
			ArgumentsKw:  common.Dict(invocation.ArgumentsKw),
		}

		resultMap, invokeErr := cb(ctx, cbInvocationMap)
		if invokeErr != nil {
			// Global error logging for any Registered WAMP topics
			log.Error().Stack().Err(invokeErr).Msgf("An error occured during invocation of %s", topic)

			return client.InvokeResult{
				Err: wamp.URI("wamp.error.canceled"), // TODO: parse Error URI from error
				Args: wamp.List{
					wamp.Dict{"error": invokeErr.Error()},
				},
			}
		}

		kwargs := resultMap.ArgumentsKw

		return client.InvokeResult{Args: resultMap.Arguments, Kwargs: wamp.Dict(kwargs)}
	}

	err := wampSession.client.Register(string(topic), invocationHandler, wamp.Dict{"force_reregister": true})
	if err != nil {
		return err
	}

	return nil
}

func (wampSession *WampSession) Unregister(topic topics.Topic) error {
	if !wampSession.Connected() {
		return ErrNotConnected
	}

	return wampSession.client.Unregister(string(topic))
}

func (wampSession *WampSession) Unsubscribe(topic topics.Topic) error {
	if !wampSession.Connected() {
		return ErrNotConnected
	}

	return wampSession.client.Unsubscribe(string(topic))
}

// SetupTestament will setup the device's testament
// This function is meant to be called once on agent start
func (wampSession *WampSession) SetupTestament() error {
	ctx := context.Background()

	config := wampSession.GetConfig()

	// https://github.com/gammazero/nexus/blob/v3/router/realm.go#L1042 on how to form payload
	args := []interface{}{
		topics.SetDeviceTestament,
		[]interface{}{
			common.Dict{
				"swarm_key":       config.ReswarmConfig.SwarmKey,
				"device_key":      config.ReswarmConfig.DeviceKey,
				"serial_number":   config.ReswarmConfig.SerialNumber,
				"wamp_session_id": wampSession.GetSessionID(),
			},
		},
		common.Dict{},
	}

	if !wampSession.Connected() {
		return ErrNotConnected
	}

	_, err := wampSession.Call(ctx, topics.MetaProcAddSessionTestament, args, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (wampSession *WampSession) Close() {
	if wampSession.client != nil {
		wampSession.client.Close() // only possible error is if it's already closed
	}
}

func clientAuthFunc(deviceSecret string) func(c *wamp.Challenge) (string, wamp.Dict) {
	return func(c *wamp.Challenge) (string, wamp.Dict) {
		return crsign.RespondChallenge(deviceSecret, c, nil), wamp.Dict{}
	}
}
