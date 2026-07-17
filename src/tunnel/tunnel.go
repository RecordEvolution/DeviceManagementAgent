package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/safe"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Protocol string

const (
	TCP   Protocol = "tcp"
	UDP   Protocol = "udp"
	HTTP  Protocol = "http"
	HTTPS Protocol = "https"
)

// TunnelCapability tracks whether this device can currently run tunnels. It is
// the per-device signal the UI uses to show/hide tunnel controls, and it lets
// syncPortState no-op cleanly (instead of erroring) when frpc is absent — e.g.
// on Windows where antivirus may quarantine frpc, or on an offline device that
// could never download it.
type TunnelCapability int32

const (
	// CapabilityUnknown: no start attempt has completed yet.
	CapabilityUnknown TunnelCapability = iota
	// CapabilityStarting: a start attempt is in progress.
	CapabilityStarting
	// CapabilityAvailable: frpc is present and logged in to the server.
	CapabilityAvailable
	// CapabilityUnavailable: frpc is missing/quarantined or repeatedly fails.
	CapabilityUnavailable
)

func (c TunnelCapability) String() string {
	switch c {
	case CapabilityStarting:
		return "starting"
	case CapabilityAvailable:
		return "available"
	case CapabilityUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

type FrpTunnelManager struct {
	TunnelManager
	tunnelsLock         *sync.RWMutex
	tunnelUpdateChan    chan TunnelUpdate
	activeTunnelConfigs map[string]*Tunnel
	clientProcess       *exec.Cmd
	configBuilder       TunnelConfigBuilder
	config              *config.Config
	messenger           messenger.Messenger
	loginChan           chan bool // Signals when frpc logs in to server
	isLoggedIn          bool
	loginMutex          sync.RWMutex

	capability   atomic.Int32
	capabilityMu sync.Mutex
	lastErr      string
	// reacquireFrpc re-fetches the frpc binary when it is found missing at
	// runtime (e.g. antivirus deleted it). Injected by the agent so the tunnel
	// package need not import system; nil in tests means "do not re-acquire".
	reacquireFrpc func() error
}

type UpdateType string

const (
	STARTED UpdateType = "started"
	REMOVED UpdateType = "removed"
)

type TunnelUpdate struct {
	DeviceKey  uint64
	AppName    string
	LocalPort  uint64
	UpdateType UpdateType
	Protocol   Protocol
}

type FullURL struct {
	HttpURL  string `json:"httpURL"`
	HttpsURL string `json:"httpsURL"`
	TcpURL   string `json:"tcpURL"`
}

type Tunnel struct {
	Error  string
	Config TunnelConfig
}

type AppTunnel struct {
	Tunnel    *Tunnel
	Mutex     sync.Mutex
	Name      string
	DeviceKey uint64
	AppKey    uint64
	Main      bool
	Public    bool
	Running   bool
}

func InterfaceToPortForwardRule(dat []interface{}) ([]common.PortForwardRule, error) {
	portEntries := make([]common.PortForwardRule, 0, len(dat))

	for _, portEntry := range dat {
		jsonStr, err := json.Marshal(portEntry)
		if err != nil {
			return nil, err
		}

		var portEntry common.PortForwardRule
		err = json.Unmarshal(jsonStr, &portEntry)
		if err != nil {
			return nil, err
		}

		if portEntry.Protocol == "" {
			portEntry.Protocol = "http"
		}

		portEntries = append(portEntries, portEntry)

	}
	return portEntries, nil
}

func PortForwardRuleToInterface(portEntries []common.PortForwardRule) ([]interface{}, error) {
	portEntriesInterface := make([]interface{}, 0, len(portEntries))

	for _, portEntry := range portEntries {
		jsonStr, err := json.Marshal(portEntry)
		if err != nil {
			return nil, err
		}

		var portEntryInterface interface{}
		err = json.Unmarshal(jsonStr, &portEntryInterface)
		if err != nil {
			return nil, err
		}

		portEntriesInterface = append(portEntriesInterface, portEntryInterface)

	}
	return portEntriesInterface, nil
}

type TunnelManager interface {
	AddTunnel(config TunnelConfig) (TunnelConfig, error)
	GetState() ([]TunnelState, error)
	GetStateById(tunnelID string) (TunnelState, error)
	RemoveTunnel(conf TunnelConfig) error
	GetTunnelConfig() ([]TunnelConfig, error)
	Get(tunnelID string) *Tunnel
	Status(tunnelID string) (TunnelStatus, error)
	Reload() error
	Start() error
	// SuperviseStart brings the client up with bounded ret/backoff; on
	// exhaustion the device settles into an unavailable-but-alive state.
	SuperviseStart()
	SaveRemotePorts(payload common.TransitionPayload) error
	// TunnelCapable reports whether tunnels can run on this device (true unless
	// definitively unavailable). syncPortState and the UI gate on this.
	TunnelCapable() bool
	// MarkUnavailable records that tunnels cannot run (e.g. an unsupported
	// platform), so the device degrades cleanly instead of retrying.
	MarkUnavailable(reason string)
}

type TunnelStatus struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	LocalAddr  string   `json:"localAddr"`
	Plugin     string   `json:"plugin"`
	RemoteAddr string   `json:"remoteAddr"`
	RemotePort uint64   `json:"remotePort"`
	Error      string   `json:"error"`
	Protocol   Protocol `json:"protocol"`
}

type FrpStatus struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Err        string `json:"err"`
	LocalAddr  string `json:"local_addr"`
	Plugin     string `json:"plugin"`
	RemoteAddr string `json:"remote_addr"`
}

type TunnelState struct {
	Status       *TunnelStatus `json:"status"`
	AppName      string        `json:"app_name"`
	Port         uint64        `json:"port"`
	Active       bool          `json:"active"`
	Error        bool          `json:"error"`
	ErrorMessage string        `json:"error_message"`
	URL          string        `json:"url"`
}

func parseProxyStatus(text string) ([]TunnelStatus, error) {

	frpStatuses := make([]TunnelStatus, 0)

	frpStatusMap := make(map[string][]FrpStatus)
	err := json.Unmarshal([]byte(text), &frpStatusMap)
	if err != nil {
		return nil, err
	}

	for _, statusArray := range frpStatusMap {
		for _, frpStatus := range statusArray {

			tunnelStatus := TunnelStatus{
				Status:     frpStatus.Status,
				Name:       frpStatus.Name,
				LocalAddr:  frpStatus.LocalAddr,
				Plugin:     frpStatus.Plugin,
				RemoteAddr: frpStatus.RemoteAddr,
				Error:      frpStatus.Err,
				Protocol:   Protocol(frpStatus.Type),
			}

			if frpStatus.RemoteAddr != "" {
				parts := strings.Split(frpStatus.RemoteAddr, ":")
				if len(parts) > 1 {
					remotePortStr := parts[1]
					remotePort, err := strconv.ParseInt(remotePortStr, 10, 64)
					if err != nil {
						log.Error().Msgf("frpStatus.RemoteAddr: %s", frpStatus.RemoteAddr)
						return nil, err
					}
					tunnelStatus.RemotePort = uint64(remotePort)
				} else {
					log.Warn().Msgf("RemoteAddr does not contain port: %s", frpStatus.RemoteAddr)
				}
			}

			frpStatuses = append(frpStatuses, tunnelStatus)
		}
	}

	return frpStatuses, nil
}

var tunnelIdRegexp = regexp.MustCompile(`\[(([^\]]+)-(http|https|tcp|udp))]`)
var errMessageRegexp = regexp.MustCompile(`error: (.*)`)
var proxyNameRegex = regexp.MustCompile(`\[(\d+)-(.*)-(\d+)-(.*)\]`)

// SetReacquireFrpc wires the callback that re-fetches frpc when it is found
// missing at runtime. Called once by the agent after construction.
func (frpTm *FrpTunnelManager) SetReacquireFrpc(fn func() error) {
	frpTm.reacquireFrpc = fn
}

// TunnelCapable reports whether tunnels can run on this device. It is true
// while a device is bringing frpc up or has it running (Unknown/Starting/
// Available) and only false once tunnels are definitively unavailable
// (frpc missing/quarantined, or repeated start failures). Gating on "not
// unavailable" — rather than "logged in" — preserves the Linux boot path,
// where an app can reconcile before frpc finishes logging in and AddTunnel/
// Reload bring the client up.
func (frpTm *FrpTunnelManager) TunnelCapable() bool {
	return TunnelCapability(frpTm.capability.Load()) != CapabilityUnavailable
}

// MarkUnavailable records that tunnels cannot run on this device (e.g. a
// platform where frpc is not yet delivered). Used instead of attempting a
// start that is known to fail.
func (frpTm *FrpTunnelManager) MarkUnavailable(reason string) {
	frpTm.setCapability(CapabilityUnavailable, errors.New(reason))
}

// Capability returns the current capability and the last error string, for
// diagnostics surfaced through get_agent_metadata.
func (frpTm *FrpTunnelManager) Capability() (TunnelCapability, string) {
	frpTm.capabilityMu.Lock()
	defer frpTm.capabilityMu.Unlock()
	return TunnelCapability(frpTm.capability.Load()), frpTm.lastErr
}

// setCapability records the capability and publishes the change so the UI
// reflects a mid-run flip (e.g. antivirus quarantining frpc) without waiting
// for a re-fetch of get_agent_metadata.
func (frpTm *FrpTunnelManager) setCapability(c TunnelCapability, err error) {
	frpTm.capabilityMu.Lock()
	prev := TunnelCapability(frpTm.capability.Load())
	frpTm.capability.Store(int32(c))
	if err != nil {
		frpTm.lastErr = err.Error()
	} else if c == CapabilityAvailable {
		frpTm.lastErr = ""
	}
	frpTm.capabilityMu.Unlock()

	if prev != c && (c == CapabilityAvailable || c == CapabilityUnavailable) {
		safe.Go(func() {
			pubErr := frpTm.PublishTunnelState()
			if pubErr != nil {
				log.Debug().Err(pubErr).Msg("failed to publish tunnel state on capability change")
			}
		})
	}
}

// ensureFrpcBinary re-acquires frpc if it is missing on disk (antivirus can
// delete it after install). Returns nil when the binary is present.
func (frpTm *FrpTunnelManager) ensureFrpcBinary() error {
	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")
	_, err := os.Stat(frpcPath)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}

	if frpTm.reacquireFrpc == nil {
		return fmt.Errorf("frpc binary missing at %s and no re-acquire available", frpcPath)
	}

	log.Warn().Msgf("frpc binary missing at %s, attempting to re-acquire", frpcPath)
	reErr := frpTm.reacquireFrpc()
	if reErr != nil {
		return fmt.Errorf("frpc binary missing and re-acquire failed: %w", reErr)
	}
	return nil
}

func (frpTm *FrpTunnelManager) Restart() error {
	log.Debug().Msg("Restarting tunnel client...")
	if frpTm.clientProcess == nil || frpTm.clientProcess.Process == nil {
		log.Error().Msg("frp client is not running")
		return errors.New("frp client is not running")
	}

	err := frpTm.clientProcess.Process.Kill()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to kill frp client process")
		return err
	}

	processState, err := frpTm.clientProcess.Process.Wait()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to wait for frp client process")
		return err
	}

	log.Debug().Msgf("Tunnel client process exited with state: %+v\n", processState)

	time.Sleep(time.Second * 5)

	return frpTm.Start()
}

func (frpTm *FrpTunnelManager) Start() error {
	log.Debug().Msg("Starting tunnel client")

	// The binary may be absent (never downloaded, or quarantined at runtime);
	// re-acquire it before spawning rather than looping on a missing file.
	err := frpTm.ensureFrpcBinary()
	if err != nil {
		frpTm.setCapability(CapabilityUnavailable, err)
		log.Error().Err(err).Msg("cannot start tunnel client")
		return err
	}

	frpTm.setCapability(CapabilityStarting, nil)

	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	ctx, cancelNotifyContext := signal.NotifyContext(context.Background(), os.Interrupt)
	frpCommand := exec.CommandContext(ctx, frpcPath, "-c", frpTm.configBuilder.ConfigPath)
	frpCommand.Dir = filepath.Dir(frpcPath)
	setPdeathsig(frpCommand)

	frpTm.clientProcess = frpCommand

	stdout, err := frpCommand.StdoutPipe()
	if err != nil {
		cancelNotifyContext()
		frpTm.setCapability(CapabilityUnavailable, err)
		log.Error().Stack().Err(err).Msg("Failed to get stdout pipe for frp command")
		return err
	}

	err = frpCommand.Start()
	if err != nil {
		cancelNotifyContext()
		frpTm.setCapability(CapabilityUnavailable, err)
		log.Error().Stack().Err(err).Msg("Failed to start frp command")
		return err
	}

	// Buffered + guarded so exactly one result is delivered: login success, or
	// the scanner ending (process exited) before login. Without the latter,
	// Start() blocked forever whenever frpc crashed before logging in.
	ackChan := make(chan error, 1)
	var ackOnce sync.Once
	ack := func(e error) { ackOnce.Do(func() { ackChan <- e }) }

	go func() {
		defer cancelNotifyContext()
		// If the loop exits without a prior ack, frpc died before login.
		defer ack(errors.New("frpc exited before logging in to the server"))

		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			line := scanner.Text()

			if strings.Contains(line, "login to server success") {
				// Mark as logged in
				frpTm.loginMutex.Lock()
				frpTm.isLoggedIn = true
				frpTm.loginMutex.Unlock()

				// Signal login (non-blocking)
				select {
				case frpTm.loginChan <- true:
				default:
				}

				// Send initial ack for startup
				ack(nil)
			}

			// Error was found
			if strings.Contains(line, "[E]") {
				tunnelIdMatch := tunnelIdRegexp.FindStringSubmatch(line)
				if len(tunnelIdMatch) > 1 {
					tunnelID := tunnelIdMatch[1]

					errMatch := errMessageRegexp.FindStringSubmatch(line)
					if len(errMatch) > 1 {
						errMessage := errMatch[1]
						frpTm.tunnelsLock.Lock()
						activeTunnel := frpTm.activeTunnelConfigs[tunnelID]
						if activeTunnel != nil {
							activeTunnel.Error = errMessage
						} else {
							log.Error().Msgf("failed to get tunnel with ID %s", tunnelID)
						}
						frpTm.tunnelsLock.Unlock()
					}

				}

				log.Error().Msgf("frp-err: %s", line)

			} else {
				proxyStarted := strings.Contains(line, "start proxy success")
				proxyRemoved := strings.Contains(line, "proxy removed")
				runAdminServerError := strings.Contains(line, "run admin server error")

				if runAdminServerError {
					log.Debug().Msgf("Tunnel process failed to setup admin server: %s", line)

					// Reset the webserver port in case the port is in use, then
					// hand control back to the supervisor via a failed ack
					// rather than recursively restarting from inside the
					// scanner goroutine (which left the outer Start() hanging).
					frpTm.configBuilder.SetAdminPort()
					ack(errors.New("frpc failed to start its admin server"))

					return
				}

				if proxyStarted || proxyRemoved {
					safe.Go(func() {
						matches := proxyNameRegex.FindStringSubmatch(line)

						if len(matches) > 1 {
							deviceKeyStr := matches[1]
							appName := matches[2]
							localPortStr := matches[3]
							protocol := matches[4]

							updateType := STARTED
							if proxyRemoved {
								updateType = REMOVED
							}

							deviceKey, err := strconv.ParseInt(deviceKeyStr, 10, 64)
							if err != nil {
								return
							}

							localPort, err := strconv.ParseInt(localPortStr, 10, 64)
							if err != nil {
								return
							}

							tunnelUpdate := TunnelUpdate{
								DeviceKey:  uint64(deviceKey),
								AppName:    appName,
								LocalPort:  uint64(localPort),
								UpdateType: updateType,
								Protocol:   Protocol(protocol),
							}

							frpTm.tunnelUpdateChan <- tunnelUpdate
						}

					})

					safe.Go(func() {
						// Wait for frpc to complete login to frps server
						// This prevents publishing "offline" state before connection is established
						frpTm.waitForLogin()

						err = frpTm.PublishTunnelState()
						if err != nil {
							log.Error().Err(err).Msg("Failed to publish tunnel state")
						}
					})
				}

				log.Info().Msgf("frp-out: %s", line)
			}
		}
	}()

	startErr := <-ackChan
	if startErr != nil {
		frpTm.setCapability(CapabilityUnavailable, startErr)
		return startErr
	}

	frpTm.setCapability(CapabilityAvailable, nil)
	return nil
}

// SuperviseStart brings the tunnel client up with bounded exponential backoff.
// It replaces "call Start() once and hope": a permanently-failing frpc (missing
// binary that can't be re-acquired, repeated login failures) settles into
// CapabilityUnavailable instead of hanging or spinning forever, so the device
// degrades cleanly. Runs to completion (success or exhaustion) — call it from a
// goroutine.
func (frpTm *FrpTunnelManager) SuperviseStart() {
	const maxAttempts = 6
	backoff := 2 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := frpTm.Start()
		if err == nil {
			log.Info().Msg("tunnel client started")
			return
		}

		log.Warn().Err(err).Msgf("tunnel client start attempt %d/%d failed", attempt, maxAttempts)
		if attempt < maxAttempts {
			time.Sleep(backoff)
			if backoff < 32*time.Second {
				backoff *= 2
			}
		}
	}

	frpTm.setCapability(CapabilityUnavailable, errors.New("tunnel client failed to start after repeated attempts"))
	log.Error().Msg("giving up starting the tunnel client; tunnels are unavailable on this device")
}

func (frpTm *FrpTunnelManager) Stop() error {
	return frpTm.clientProcess.Process.Kill()
}

func (frpTm *FrpTunnelManager) Reset() error {
	frpTm.configBuilder.Reset()

	return frpTm.Reload()
}

// waitForLogin waits for frpc to successfully login to frps server
// If already logged in, returns immediately
// If not logged in, waits up to 5 seconds for login confirmation
func (frpTm *FrpTunnelManager) waitForLogin() {
	frpTm.loginMutex.RLock()
	if frpTm.isLoggedIn {
		frpTm.loginMutex.RUnlock()
		log.Debug().Msg("frpc already logged in, publishing tunnel state immediately")
		return
	}
	frpTm.loginMutex.RUnlock()

	log.Debug().Msg("Waiting for frpc login confirmation before publishing tunnel state")

	// Wait for login signal with timeout
	select {
	case <-frpTm.loginChan:
		log.Info().Msg("frpc login confirmed, proceeding with tunnel state publication")
	case <-time.After(5 * time.Second):
		log.Warn().Msg("Timeout waiting for frpc login, publishing tunnel state anyway")
	}
}

func (frpTm *FrpTunnelManager) PublishTunnelState() error {
	updateTopic := common.BuildTunnelStateUpdate(frpTm.config.ReswarmConfig.SerialNumber)
	tunnelStates, err := frpTm.GetState()
	if err != nil {
		return err
	}

	var args []interface{}
	for _, tunnelState := range tunnelStates {
		args = append(args, tunnelState)
	}

	return frpTm.messenger.Publish(topics.Topic(updateTopic), args, nil, nil)
}

func NewFrpTunnelManager(messenger messenger.Messenger, config *config.Config) (*FrpTunnelManager, error) {

	configBuilder := NewTunnelConfigBuilder(config)
	frpTunnelManager := &FrpTunnelManager{
		configBuilder:       configBuilder,
		messenger:           messenger,
		config:              config,
		tunnelsLock:         &sync.RWMutex{},
		tunnelUpdateChan:    make(chan TunnelUpdate),
		activeTunnelConfigs: make(map[string]*Tunnel),
		loginChan:           make(chan bool, 10), // Buffered to avoid blocking
		isLoggedIn:          false,
	}

	return frpTunnelManager, nil
}

func (frpTm *FrpTunnelManager) SetMessenger(messenger messenger.Messenger) {
	frpTm.messenger = messenger
}

func (frpTm *FrpTunnelManager) reserveRemotePort(port uint64, protocol Protocol) (uint64, error) {
	args := []interface{}{
		common.Dict{
			"port":     port,
			"protocol": string(protocol),
		},
	}

	result, err := frpTm.messenger.Call(context.Background(), topics.ExposePort, args, common.Dict{}, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate value") {
			log.Debug().Msg("Port still exposed in backend, continuing...")
			return port, nil
		}
		return 0, err
	}

	if result.Arguments == nil || len(result.Arguments) == 0 {
		return 0, errors.New("arguments is empty")
	}

	payloadArg := result.Arguments[0]
	payload, ok := payloadArg.(map[string]interface{})
	if !ok {
		return 0, errors.New("failed to parse payload")
	}

	remotePortKw := payload["remote_port"]
	remotePort, ok := remotePortKw.(uint64)
	if !ok {
		return 0, errors.New("failed to parse port")
	}

	return remotePort, nil
}

func (frpTm *FrpTunnelManager) Reload() error {
	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	output, err := exec.CommandContext(ctx, frpcPath, "reload", "-c", frpTm.configBuilder.ConfigPath).CombinedOutput()
	if err != nil {
		// frpc service is not properly running
		log.Error().Err(err).Msgf("Error while reloading frp client config: %s", string(output))
		if strings.Contains(string(output), "connect: connection refused") || strings.Contains(string(output), "api status code") {
			log.Warn().Msg("frpc appears to not be running, attempting to start it")
			safe.Go(func() {
				// Re-supervise so a binary that was quarantined/removed at
				// runtime is re-acquired and the capability flips correctly.
				frpTm.SuperviseStart()
			})
			return err
		}
		return err
	}

	return nil
}

func (frpTm *FrpTunnelManager) Get(tunnelID string) *Tunnel {
	frpTm.tunnelsLock.Lock()
	tunnel := frpTm.activeTunnelConfigs[tunnelID]
	frpTm.tunnelsLock.Unlock()

	return tunnel
}

func (frpTm *FrpTunnelManager) buildURL(protocol Protocol, subdomain string, remotePort uint64) string {
	protocolString := string(protocol)

	if remotePort != 0 && protocol != HTTP && protocol != HTTPS {
		return fmt.Sprintf("%s://%s.%s:%d", protocolString, subdomain, frpTm.configBuilder.BaseTunnelURL, remotePort)
	}

	// we always have HTTPS since we tunnel to our HTTPS service
	if protocolString == "http" {
		protocolString = "https"
	}

	return fmt.Sprintf("%s://%s.%s", protocolString, subdomain, frpTm.configBuilder.BaseTunnelURL)
}

func (frpTm *FrpTunnelManager) GetState() ([]TunnelState, error) {
	tunnelConfigs, err := frpTm.GetTunnelConfig()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get tunnel configs")
		return nil, err
	}

	tunnelStatuses, err := frpTm.AllStatus()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get tunnel statuses")
		return nil, err
	}

	tunnelStates := make([]TunnelState, 0)
	for _, tunnelConfig := range tunnelConfigs {
		// Match on the name frpc reports status under. Rebuilding the id from
		// Subdomain silently drops any proxy whose subdomain did not round-trip
		// through the config file.
		tunnelID := tunnelConfig.Name
		if tunnelID == "" {
			tunnelID = CreateTunnelID(tunnelConfig.Subdomain, string(tunnelConfig.Protocol))
		}

		for _, tunnelStatus := range tunnelStatuses {
			if tunnelID != tunnelStatus.Name {
				continue
			}

			tunnelState := TunnelState{
				Status: &tunnelStatus,
				// The declared port, NOT the host port frpc dials: the UI
				// matches tunnel states to port-template entries by this
				// number.
				Port:         tunnelConfig.DeclaredPort,
				AppName:      tunnelConfig.AppName,
				Error:        tunnelStatus.Error != "",
				ErrorMessage: tunnelStatus.Error,
				Active:       tunnelStatus.Status == "running",
				URL:          frpTm.buildURL(tunnelConfig.Protocol, tunnelConfig.Subdomain, tunnelStatus.RemotePort),
			}

			tunnelStates = append(tunnelStates, tunnelState)
		}

	}

	return tunnelStates, nil
}

func (frpTm *FrpTunnelManager) GetStateById(tunnelID string) (TunnelState, error) {
	states, err := frpTm.GetState()
	if err != nil {
		return TunnelState{}, err
	}

	for _, state := range states {
		if state.Status.Name == tunnelID {
			return state, nil
		}
	}

	return TunnelState{}, errdefs.ErrNotFound
}

func (frpTm *FrpTunnelManager) GetTunnelConfig() ([]TunnelConfig, error) {
	return frpTm.configBuilder.GetTunnelConfig()
}

func (frpTm *FrpTunnelManager) AllStatus() ([]TunnelStatus, error) {
	// Only poll the admin API while frpc is supposed to be up. When the client
	// is Unavailable (frps unreachable, binary quarantined) or still Starting,
	// the connection-refused below would kick off yet another restart — on
	// devices without a reachable frps that turned into an endless
	// kill/sleep/start loop, retriggered by every state publish.
	if capability, _ := frpTm.Capability(); capability != CapabilityAvailable {
		return []TunnelStatus{}, nil
	}

	adminPort, err := frpTm.configBuilder.GetAdminPort()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get admin port from config")
		return []TunnelStatus{}, err
	}

	apiURL := fmt.Sprintf("http://127.0.0.1:%d/api/status", adminPort)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP request")
		return []TunnelStatus{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			safe.Go(func() {
				frpTm.Restart()
			})
			return []TunnelStatus{}, nil
		}
		log.Error().Err(err).Msg("Failed to call frpc API")
		return []TunnelStatus{}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Msgf("frpc API returned status code: %d", resp.StatusCode)
		return []TunnelStatus{}, fmt.Errorf("API returned status code: %d", resp.StatusCode)
	}

	var statusResp map[string][]FrpStatus
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		log.Error().Err(err).Msg("Failed to decode JSON response")
		return []TunnelStatus{}, err
	}

	// Convert map to flat array
	tunnelStatuses := make([]TunnelStatus, 0)
	for _, statusArray := range statusResp {
		for _, frpStatus := range statusArray {
			tunnelStatus := TunnelStatus{
				Status:     frpStatus.Status,
				Name:       frpStatus.Name,
				LocalAddr:  frpStatus.LocalAddr,
				Plugin:     frpStatus.Plugin,
				RemoteAddr: frpStatus.RemoteAddr,
				Error:      frpStatus.Err,
				Protocol:   Protocol(frpStatus.Type),
			}

			if frpStatus.RemoteAddr != "" {
				parts := strings.Split(frpStatus.RemoteAddr, ":")
				if len(parts) > 1 {
					remotePortStr := parts[1]
					remotePort, err := strconv.ParseInt(remotePortStr, 10, 64)
					if err != nil {
						log.Error().Msgf("frpStatus.RemoteAddr: %s", frpStatus.RemoteAddr)
						return nil, err
					}
					tunnelStatus.RemotePort = uint64(remotePort)
				} else {
					log.Warn().Msgf("RemoteAddr does not contain port: %s", frpStatus.RemoteAddr)
				}
			}

			tunnelStatuses = append(tunnelStatuses, tunnelStatus)
		}
	}

	return tunnelStatuses, nil
}

func (frpTm *FrpTunnelManager) Status(tunnelID string) (TunnelStatus, error) {
	// Use AllStatus and filter for the specific tunnel
	tunnelStatuses, err := frpTm.AllStatus()
	if err != nil {
		log.Error().Err(err).Msg("Error while getting tunnel status")
		return TunnelStatus{}, err
	}

	for _, tunnelStatus := range tunnelStatuses {
		if tunnelStatus.Name == tunnelID {
			return tunnelStatus, nil
		}
	}

	return TunnelStatus{}, errdefs.ErrNotFound
}

// Tries to reserve an external port for kubernetes, updates the frpc client config and reloads the config file
func (frpTm *FrpTunnelManager) AddTunnel(config TunnelConfig) (TunnelConfig, error) {
	log.Debug().Str("subdomain", config.Subdomain).Str("protocol", string(config.Protocol)).Msg("AddTunnel called")
	// Don't need to reserve a port if the user starts an HTTP tunnel
	if config.Protocol != HTTP && config.Protocol != HTTPS {
		// If no remote port is set, we will allocate one
		remotePort, err := frpTm.reserveRemotePort(config.RemotePort, config.Protocol)
		if err != nil {
			log.Error().Err(err).Msg("Error while reserving remote port")
			return TunnelConfig{}, err
		}
		config.RemotePort = remotePort
	}

	frpTm.configBuilder.AddTunnelConfig(config)

	err := frpTm.Reload()
	if err != nil {
		// Rollback the config change if reload fails
		frpTm.configBuilder.RemoveTunnelConfig(config)
		return TunnelConfig{}, err
	}

	// for update := range frpTm.tunnelUpdateChan {
	// 	if update.AppName == strings.ToLower(config.AppName) &&
	// 		update.LocalPort == config.LocalPort &&
	// 		update.Protocol == config.Protocol &&
	// 		update.UpdateType == STARTED {
	// 		break
	// 	}
	// }

	tunnelId := CreateTunnelID(config.Subdomain, string(config.Protocol))
	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] == nil {
		frpTm.activeTunnelConfigs[tunnelId] = &Tunnel{Config: config}
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return TunnelConfig{}, errors.New("tunnel already exists")
	}

	return config, nil
}

func (frpTm *FrpTunnelManager) RemoveTunnel(conf TunnelConfig) error {
	log.Debug().Str("subdomain", conf.Subdomain).Str("protocol", string(conf.Protocol)).Msg("RemoveTunnel called")
	tunnelId := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	// Remove from active tunnels in memory (if exists)
	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] != nil {
		delete(frpTm.activeTunnelConfigs, tunnelId)
		log.Debug().Str("tunnelId", tunnelId).Msg("Removed tunnel from active configs")
	} else {
		log.Debug().Str("tunnelId", tunnelId).Msg("Tunnel not in active configs, but will remove from config file")
	}
	frpTm.tunnelsLock.Unlock()

	// Always remove from config file regardless of whether it's in memory
	frpTm.configBuilder.RemoveTunnelConfig(conf)

	err := frpTm.Reload()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to reload after removing tunnel")
		return err
	}

	return nil
}

func (frpTm *FrpTunnelManager) SaveRemotePorts(payload common.TransitionPayload) error {
	// log.Debug().Str("app_key", fmt.Sprintf("%v", payload.AppKey)).Interface("payload", payload).Msg("SaveRemotePort called")

	update := []interface{}{common.Dict{
		"app_key":    payload.AppKey,
		"device_key": frpTm.config.ReswarmConfig.DeviceKey,
		"swarm_key":  frpTm.config.ReswarmConfig.SwarmKey,
		"stage":      payload.Stage,
		"ports":      payload.Ports,
		"foo":        "bar",
	}}
	log.Debug().Interface("update", update).Msg("Saving remote port with update payload")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, err := frpTm.messenger.Call(ctx, topics.SetActualAppOnDeviceState, update, nil, nil, nil)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to save remote port")
		return err
	}

	log.Info().Str("app_key", fmt.Sprintf("%v", payload.AppKey)).Msg("Remote port saved successfully")
	return nil
}

func init() {
	// Set zerolog to use a pretty console writer for human-friendly logs
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"})
}
