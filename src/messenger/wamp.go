package messenger

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
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
	client         NexusClient
	container      container.Container
	agentConfig    *config.Config
	socketConfig   *SocketConfig
	clientProvider ClientProvider // nil means use default client.ConnectNet
	mu             sync.Mutex
	onConnect      func(reconnect bool) // Callback invoked after successful connection/reconnection
	heartbeatDone  chan struct{}        // Closed by heartbeat when connection failure is detected
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
	// Check for connection errors that indicate the connection is broken
	for _, val := range v {
		if str, ok := val.(string); ok {
			if strings.Contains(str, "broken pipe") || strings.Contains(str, "connection reset") || strings.Contains(str, "write tcp") {
				log.Warn().Msgf("WAMP connection error detected: %v", str)
			}
		}
	}
	wl.logger.Print(v...)
}

func (wl wampLogWrapper) Println(v ...interface{}) {
	// Check for connection errors
	for _, val := range v {
		if str, ok := val.(string); ok {
			if strings.Contains(str, "broken pipe") || strings.Contains(str, "connection reset") || strings.Contains(str, "write tcp") {
				log.Warn().Msgf("WAMP connection error detected: %v", str)
			}
		}
	}
	wl.logger.Print(v, "\n")
}

func (wl wampLogWrapper) Printf(format string, v ...interface{}) {
	// Check for connection errors in formatted strings
	if strings.Contains(format, "broken pipe") || strings.Contains(format, "connection reset") || strings.Contains(format, "write tcp") {
		log.Warn().Msgf("WAMP connection error detected: "+format, v...)
	}
	wl.logger.Printf(format, v...)
}

func wrapZeroLogger(zeroLogger zerolog.Logger) wampLogWrapper {
	wrapper := newWampLogger(&zeroLogger)
	return wrapper
}

type SocketConfig struct {
	PingPongTimeout   time.Duration
	ResponseTimeout   time.Duration
	ConnectionTimeout time.Duration
	HeartbeatInterval time.Duration // Default: 30 seconds if zero
	SetupTestament    bool
}

// DefaultHeartbeatInterval is the default interval between heartbeat checks
const DefaultHeartbeatInterval = 30 * time.Second

// ClientProvider is a function that attempts to connect and returns a NexusClient.
// It's used to allow mocking the connection in tests.
type ClientProvider func(ctx context.Context, url string, cfg client.Config) (NexusClient, error)

var legacyEndpointRegex = regexp.MustCompile(`devices.*\.(com|io):8080`)

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

	isLegacyEndpoint := legacyEndpointRegex.Match([]byte(reswarmConfig.DeviceEndpointURL))
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

// SetOnConnect sets the callback that will be invoked after each successful connection or reconnection.
// The callback receives a boolean indicating whether this is a reconnection (true) or initial connection (false).
func (wampSession *WampSession) SetOnConnect(cb func(reconnect bool)) {
	wampSession.mu.Lock()
	defer wampSession.mu.Unlock()
	wampSession.onConnect = cb
}

// NewWampSession creates a new WampSession from a ReswarmConfig file.
// If clientProvider is nil, the default client.ConnectNet is used.
func NewWampSession(config *config.Config, socketConfig *SocketConfig, container container.Container, clientProvider ClientProvider) (*WampSession, error) {
	session := &WampSession{
		agentConfig:    config,
		socketConfig:   socketConfig,
		container:      container,
		clientProvider: clientProvider,
		heartbeatDone:  make(chan struct{}),
	}

	err := session.connect()
	if err != nil {
		return nil, err
	}

	return session, nil
}

// connect establishes the WAMP connection
func (wampSession *WampSession) connect() error {
	clientChannel := wampSession.establishSocketConnection()
	wampSession.client = <-clientChannel

	if wampSession.socketConfig.SetupTestament {
		err := wampSession.SetupTestament()
		if err != nil {
			return err
		}
	}

	// Start connection monitoring
	wampSession.listenForDisconnect()
	wampSession.startHeartbeat()

	return nil
}

// reconnect attempts to re-establish the WAMP connection.
// Returns true if reconnection was successful, false otherwise.
func (wampSession *WampSession) reconnect() bool {
	wampSession.mu.Lock()
	// Close existing client
	if wampSession.client != nil {
		wampSession.client.Close()
		wampSession.client = nil
	}
	wampSession.mu.Unlock()

	// Establish new connection
	clientChannel := wampSession.establishSocketConnection()

	wampSession.mu.Lock()
	wampSession.client = <-clientChannel
	wampSession.mu.Unlock()

	// Check if we actually connected
	if !wampSession.Connected() {
		return false
	}

	if wampSession.socketConfig.SetupTestament {
		err := wampSession.SetupTestament()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup testament")
		}
	}

	return true
}

// listenForDisconnect starts a goroutine that monitors for disconnection
// and automatically handles reconnection. When reconnection succeeds,
// it invokes the onConnect callback (if set) with reconnect=true.
func (wampSession *WampSession) listenForDisconnect() {
	log.Debug().Msg("listenForDisconnect: Starting disconnect listener setup...")

	// Get the current client's done channel
	wampSession.mu.Lock()
	client := wampSession.client
	heartbeatDone := wampSession.heartbeatDone
	wampSession.mu.Unlock()

	if client == nil {
		log.Warn().Msg("ListenForDisconnect called but client is nil")
		return
	}

	clientDone := client.Done()

	safe.Go(func() {
		// Wait for either the client to signal done OR the heartbeat to detect failure
		select {
		case <-clientDone:
			log.Warn().Msg("Connection lost: Received done signal from WAMP client (broken pipe, timeout, or server disconnect)")
		case <-heartbeatDone:
			log.Warn().Msg("Connection lost: Heartbeat detected connection failure")
		}

		wampSession.Close()

		// Cancel any active container streams
		if wampSession.container != nil {
			err := wampSession.container.CancelAllStreams()
			if err != nil {
				log.Error().Err(err).Msg("error closing stream")
			}
		}

		reconnectAttempt := 0
		for {
			reconnectAttempt++
			log.Info().Msgf("Reconnect attempt #%d: Attempting to reconnect...", reconnectAttempt)

			if wampSession.reconnect() {
				log.Info().Msgf("Reconnect attempt #%d: Successfully reconnected to WAMP router", reconnectAttempt)

				// Invoke onConnect callback if set
				wampSession.mu.Lock()
				cb := wampSession.onConnect
				wampSession.mu.Unlock()

				if cb != nil {
					safe.Go(func() {
						log.Info().Msg("Re-initializing after reconnection...")
						cb(true) // reconnect = true
						log.Info().Msg("Successfully re-initialized after reconnection")
					})
				}

				// Start listening for the next disconnect and restart heartbeat
				wampSession.listenForDisconnect()
				return
			}

			log.Warn().Msgf("Reconnect attempt #%d: Connection failed or immediately lost, retrying in 1s...", reconnectAttempt)
			time.Sleep(time.Second * 1)
		}
	})

	log.Debug().Msg("Messenger: Setup disconnect listener")
}

// startHeartbeat starts a goroutine that periodically sends a heartbeat to verify
// the connection is alive. If the heartbeat fails consecutively, it signals via heartbeatDone channel.
func (wampSession *WampSession) startHeartbeat() {
	heartbeatInterval := wampSession.socketConfig.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = DefaultHeartbeatInterval
	}

	// Create a new heartbeat channel only if the current one is nil or closed
	wampSession.mu.Lock()
	if wampSession.heartbeatDone == nil {
		wampSession.heartbeatDone = make(chan struct{})
	} else {
		// Check if channel is closed by trying a non-blocking receive
		select {
		case <-wampSession.heartbeatDone:
			// Channel was closed, create a new one
			wampSession.heartbeatDone = make(chan struct{})
		default:
			// Channel is still open, keep using it
		}
	}
	heartbeatDone := wampSession.heartbeatDone
	wampSession.mu.Unlock()

	safe.Go(func() {
		consecutiveFailures := 0
		const maxConsecutiveFailures = 2 // Trigger reconnect after 2 failures

		for {
			time.Sleep(heartbeatInterval)

			// Check if connection is still alive
			if !wampSession.Connected() {
				log.Warn().Msg("Connection lost detected in heartbeat, waiting for reconnection...")
				consecutiveFailures = 0
				continue
			}

			err := wampSession.UpdateRemoteDeviceStatus(CONNECTED)
			if err != nil {
				consecutiveFailures++
				log.Warn().Err(err).Msgf("Failed to send heartbeat (%d/%d failures), connection may be lost", consecutiveFailures, maxConsecutiveFailures)

				// If we've failed multiple times, the connection is likely broken
				// Signal via heartbeatDone channel to trigger reconnection
				if consecutiveFailures >= maxConsecutiveFailures {
					log.Error().Msg("Connection appears to be broken (multiple heartbeat failures), signaling reconnection...")
					close(heartbeatDone)
					return // Exit this heartbeat goroutine
				}
			} else {
				// Reset counter on success
				if consecutiveFailures > 0 {
					log.Info().Msg("Heartbeat successful, connection restored")
				}
				consecutiveFailures = 0
			}
		}
	})

	log.Debug().Msg("Messenger: Started heartbeat")
}

func (wampSession *WampSession) Publish(topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict) error {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return ErrNotConnected
	}

	err := client.Publish(string(topic), wamp.Dict(options), args, wamp.Dict(kwargs))
	if err != nil {
		log.Debug().Err(err).Str("topic", string(topic)).Msg("Failed to publish to topic")
	}
	return err
}

// establishSocketConnection establishes a WAMP connection using the session's ClientProvider.
// If clientProvider is nil, client.ConnectNet is used directly.
func (wampSession *WampSession) establishSocketConnection() chan NexusClient {
	agentConfig := wampSession.agentConfig
	socketConfig := wampSession.socketConfig
	container := wampSession.container
	provider := wampSession.clientProvider

	resChan := make(chan NexusClient)

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
			var wClient NexusClient
			if provider != nil {
				wClient, err = provider(ctx, agentConfig.ReswarmConfig.DeviceEndpointURL, *connectionConfig)
			} else {
				wClient, err = client.ConnectNet(ctx, agentConfig.ReswarmConfig.DeviceEndpointURL, *connectionConfig)
			}
			if err != nil {
				if cancelFunc != nil {
					cancelFunc()
				}

				duration = time.Since(requestStart)

				if strings.Contains(err.Error(), "WAMP-CRA client signature is invalid") {
					exitMessage := fmt.Sprintln("The IronFlock device connect authentication failed")
					fmt.Println(exitMessage)
					os.Exit(1)
				}

				log.Debug().Stack().Err(err).Msgf("Failed to establish a websocket connection (duration: %s), reattempting... in 1s", duration.String())
				time.Sleep(time.Second * 1)
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
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()
	if client == nil {
		return false
	}
	return client.Connected()
}

// Client returns the underlying NexusClient.
// This is primarily useful for testing to access the MockNexusClient.
func (wampSession *WampSession) Client() NexusClient {
	wampSession.mu.Lock()
	defer wampSession.mu.Unlock()
	return wampSession.client
}

func (wampSession *WampSession) Subscribe(topic topics.Topic, cb func(Result) error, options common.Dict) error {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return ErrNotConnected
	}

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

	return client.Subscribe(string(topic), handler, wamp.Dict(options))
}

func (wampSession *WampSession) GetConfig() *config.Config {
	return wampSession.agentConfig
}

func (wampSession *WampSession) SubscriptionID(topic topics.Topic) (id uint64, ok bool) {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return 0, false
	}
	subID, ok := client.SubscriptionID(string(topic))
	return uint64(subID), ok
}

func (wampSession *WampSession) RegistrationID(topic topics.Topic) (id uint64, ok bool) {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return 0, false
	}
	subID, ok := client.RegistrationID(string(topic))
	return uint64(subID), ok
}

func (wampSession *WampSession) Call(
	ctx context.Context,
	topic topics.Topic,
	args []interface{},
	kwargs common.Dict,
	options common.Dict,
	progCb func(Result)) (Result, error) {

	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
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

	result, err := client.Call(ctx, string(topic), wamp.Dict(options), args, wamp.Dict(kwargs), handler)
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
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return 0
	}

	return uint64(client.ID())
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

	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return ErrNotConnected
	}

	err := client.Register(string(topic), invocationHandler, wamp.Dict{"force_reregister": true})
	if err != nil {
		return err
	}

	return nil
}

func (wampSession *WampSession) Unregister(topic topics.Topic) error {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return ErrNotConnected
	}

	return client.Unregister(string(topic))
}

func (wampSession *WampSession) Unsubscribe(topic topics.Topic) error {
	wampSession.mu.Lock()
	client := wampSession.client
	wampSession.mu.Unlock()

	if client == nil {
		return ErrNotConnected
	}

	return client.Unsubscribe(string(topic))
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
	wampSession.mu.Lock()
	clientToClose := wampSession.client
	wampSession.client = nil
	wampSession.mu.Unlock()

	if clientToClose != nil {
		clientToClose.Close()
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

	stats := common.GetStats()

	payload := common.Dict{
		"swarm_key":       config.ReswarmConfig.SwarmKey,
		"device_key":      config.ReswarmConfig.DeviceKey,
		"status":          string(status),
		"wamp_session_id": wampSession.GetSessionID(),
		"stats": common.Dict{
			"cpu_count":           stats.CPUCount,
			"cpu_usage":           stats.CPUUsagePercent,
			"memory_total":        stats.MemoryTotal,
			"memory_used":         stats.MemoryUsed,
			"memory_available":    stats.MemoryAvailable,
			"storage_total":       stats.StorageTotal,
			"storage_used":        stats.StorageUsed,
			"storage_free":        stats.StorageFree,
			"docker_apps_total":   stats.DockerAppsTotal,
			"docker_apps_used":    stats.DockerAppsUsed,
			"docker_apps_free":    stats.DockerAppsFree,
			"docker_apps_mounted": stats.DockerAppsMounted,
		},
	}

	res, err := wampSession.Call(ctx, topics.UpdateDeviceStatus, []any{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	if res.Arguments == nil || res.Arguments[0] == nil {
		return nil
	}

	args, ok := res.Arguments[0].(map[string]any)
	if !ok {
		return nil
	}

	reswarmBaseURL := fmt.Sprint(args["reswarmBaseURL"])
	if reswarmBaseURL != "" {
		wampSession.agentConfig.ReswarmConfig.ReswarmBaseURL = reswarmBaseURL
	}

	return nil
}
