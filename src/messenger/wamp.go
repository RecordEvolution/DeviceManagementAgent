package messenger

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"reagent/common"
	"reagent/config"
	"reagent/container"
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
	client       *client.Client
	container    container.Container
	agentConfig  *config.Config
	socketConfig *SocketConfig
}

type wampLogWrapper struct {
	logger *zerolog.Logger
}

type DeviceStatus string

const (
	CONNECTED    DeviceStatus = "CONNECTED"
	DISCONNECTED DeviceStatus = "DISCONNECTED"
	CONFIGURING  DeviceStatus = "CONFIGURING"
)

var ErrNotConnected = errors.New("not connected")

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

type SocketConfig struct {
	PingPongTimeout   time.Duration
	ResponseTimeout   time.Duration
	ConnectionTimeout time.Duration
	SetupTestament    bool
}

func createConnectConfig(config *config.Config, socketConfig *SocketConfig) (*client.Config, error) {
	reswarmConfig := config.ReswarmConfig

	cfg := client.Config{
		Realm: "realm1",
		HelloDetails: wamp.Dict{
			"authid": fmt.Sprintf("%d-%d", reswarmConfig.SwarmKey, reswarmConfig.DeviceKey),
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc(reswarmConfig.Secret),
		},
		Debug:  config.CommandLineArguments.DebugMessaging,
		Logger: wrapZeroLogger(log.Logger),
	}

	isLegacyEndpoint, err := regexp.Match(`devices.*\.(com|io):8080`, []byte(reswarmConfig.DeviceEndpointURL))
	if err != nil {
		return nil, err
	}

	if isLegacyEndpoint {
		tlscert, err := tls.X509KeyPair([]byte(reswarmConfig.Authentication.Certificate), []byte(reswarmConfig.Authentication.Key))
		if err != nil {
			return nil, err
		}

		cfg.TlsCfg = &tls.Config{
			Certificates:       []tls.Certificate{tlscert},
			InsecureSkipVerify: true,
		}
	}

	if socketConfig.PingPongTimeout != 0 {
		cfg.WsCfg = transport.WebsocketConfig{
			KeepAlive: socketConfig.PingPongTimeout,
		}
	}

	if socketConfig.ResponseTimeout != 0 {
		cfg.ResponseTimeout = socketConfig.ResponseTimeout
	}

	return &cfg, nil
}

// New creates a new wamp session from a ReswarmConfig file
func NewWamp(config *config.Config, socketConfig *SocketConfig, container container.Container) (*WampSession, error) {
	session := &WampSession{agentConfig: config, socketConfig: socketConfig, container: container}
	clientChannel := EstablishSocketConnection(config, socketConfig, container)

	select {
	case client := <-clientChannel:
		session.client = client
	}

	if session.socketConfig.SetupTestament {
		err := session.SetupTestament()
		if err != nil {
			return nil, err
		}
	}

	return session, nil
}

func (wampSession *WampSession) Reconnect() {
	wampSession.Close()

	clientChannel := EstablishSocketConnection(wampSession.agentConfig, wampSession.socketConfig, wampSession.container)
	select {
	case client := <-clientChannel:
		wampSession.client = client
	}

	if wampSession.socketConfig.SetupTestament {
		err := wampSession.SetupTestament()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup testament")
		}
	}
}

func (wampSession *WampSession) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	if !wampSession.Connected() {
		return ErrNotConnected
	}

	return wampSession.client.Publish(string(topic), wamp.Dict(options), args, wamp.Dict(kwargs))
}

func EstablishSocketConnection(agentConfig *config.Config, socketConfig *SocketConfig, container container.Container) chan *client.Client {
	resChan := make(chan *client.Client)

	// never returns a established connection
	if agentConfig.CommandLineArguments.Offline {
		log.Warn().Msg("Started in offline mode, will not establish a socket connection!")
		return resChan
	}

	log.Debug().Msg("Attempting to establish a socket connection...")

	safe.Go(func() {
		for {
			connectionConfig, err := createConnectConfig(agentConfig, socketConfig)
			if err != nil {
				log.Error().Err(err).Msg("failed to create connect config...")
				continue
			}

			var ctx context.Context
			var cancelFunc context.CancelFunc

			if socketConfig.ConnectionTimeout == 0 {
				ctx = context.Background()
			} else {
				ctx, cancelFunc = context.WithTimeout(context.Background(), socketConfig.ConnectionTimeout)
			}

			var duration time.Duration
			requestStart := time.Now() // time request
			wClient, err := client.ConnectNet(ctx, agentConfig.ReswarmConfig.DeviceEndpointURL, *connectionConfig)
			if err != nil {
				if cancelFunc != nil {
					cancelFunc()
				}

				duration = time.Since(requestStart)

				if strings.Contains(err.Error(), "WAMP-CRA client signature is invalid") {
					exitMessage := fmt.Sprintln("The RESWARM device no longer exists")
					fmt.Println(exitMessage)
					os.Exit(1)
				}

				log.Debug().Stack().Err(err).Msgf("Failed to establish a websocket connection (duration: %s), reattempting... in 100ms", duration.String())
				time.Sleep(time.Millisecond * 100)
				continue
			}

			if cancelFunc != nil {
				cancelFunc()
			}

			// add a dummy topic that will be used as a means to check if a client has an existing session or not
			topic := common.BuildExternalApiTopic(agentConfig.ReswarmConfig.SerialNumber, "wamp_connection_established")
			invokeHandler := func(ctx context.Context, i *wamp.Invocation) client.InvokeResult {
				return client.InvokeResult{Args: wamp.List{"Hello :-)"}}
			}

			err = wClient.Register(topic, invokeHandler, nil)
			if err != nil && strings.Contains(err.Error(), string(wamp.ErrProcedureAlreadyExists)) {
				exitMessage := fmt.Sprintf("a WAMP connection for %s already exists", agentConfig.ReswarmConfig.SerialNumber)
				fmt.Println(exitMessage)
				os.Exit(1)
			}

			onDestroyListener := func(event *wamp.Event) {
				container.PruneSystem()
				os.Exit(1)
			}

			wClient.Subscribe(fmt.Sprintf("%s/ondestroy", topics.ReswarmDeviceList), onDestroyListener, wamp.Dict{})

			if wClient.Connected() {
				duration = time.Since(requestStart)
				log.Debug().Msgf("Sucessfully established a connection (duration: %s)", duration.String())
				resChan <- wClient
				close(resChan)
				return
			}

			duration = time.Since(requestStart)
			if wClient != nil {
				wClient.Close()
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

	return wampSession.client.Subscribe(string(topic), handler, wamp.Dict(options))
}

func (wampSession *WampSession) GetConfig() *config.Config {
	return wampSession.agentConfig
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

	if !wampSession.Connected() {
		return Result{}, ErrNotConnected
	}

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
	return wampSession.client.Unregister(string(topic))
}

func (wampSession *WampSession) Unsubscribe(topic topics.Topic) error {
	return wampSession.client.Unsubscribe(string(topic))
}

// SetupTestament will setup the device's crossbar testament
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

	_, err := wampSession.Call(ctx, topics.MetaProcAddSessionTestament, args, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (wampSession *WampSession) Close() {
	if wampSession.client != nil {
		wampSession.client.Close() // only possible error is if it's already closed
		wampSession.client = nil
	}
}

func clientAuthFunc(deviceSecret string) func(c *wamp.Challenge) (string, wamp.Dict) {
	return func(c *wamp.Challenge) (string, wamp.Dict) {
		return crsign.RespondChallenge(deviceSecret, c, nil), wamp.Dict{}
	}
}

func (wampSession *WampSession) UpdateRemoteDeviceStatus(status DeviceStatus) error {
	config := wampSession.GetConfig()
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	payload := common.Dict{
		"swarm_key":       config.ReswarmConfig.SwarmKey,
		"device_key":      config.ReswarmConfig.DeviceKey,
		"status":          string(status),
		"wamp_session_id": wampSession.GetSessionID(),
	}

	res, err := wampSession.Call(ctx, topics.UpdateDeviceStatus, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	if res.Arguments == nil || res.Arguments[0] == nil {
		return nil
	}

	args, ok := res.Arguments[0].(map[string]interface{})
	if !ok {
		return nil
	}

	reswarmBaseURL := fmt.Sprint(args["reswarmBaseURL"])
	if reswarmBaseURL != "" {
		wampSession.agentConfig.ReswarmConfig.ReswarmBaseURL = reswarmBaseURL
	}

	return nil
}
