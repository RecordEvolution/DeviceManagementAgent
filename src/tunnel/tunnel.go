package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type FrpTunnelManager struct {
	TunnelManager
	tunnelsLock         *sync.RWMutex
	tunnelUpdateChan    chan TunnelUpdate
	activeTunnelConfigs map[string]*Tunnel
	clientProcess       *exec.Cmd
	configBuilder       TunnelConfigBuilder
	config              *config.Config
	messenger           messenger.Messenger
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
	SaveRemotePorts(payload common.TransitionPayload) error
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

	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	ctx, cancelNotifyContext := signal.NotifyContext(context.Background(), os.Interrupt)
	frpCommand := exec.CommandContext(ctx, frpcPath)
	frpCommand.Dir = filepath.Dir(frpcPath)

	frpTm.clientProcess = frpCommand

	stdout, err := frpCommand.StdoutPipe()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get stdout pipe for frp command")
		return err
	}

	err = frpCommand.Start()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to start frp command")
		return err
	}

	ackChan := make(chan bool)
	go func() {
		defer cancelNotifyContext()

		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			line := scanner.Text()

			if strings.Contains(line, "login to server success") {
				ackChan <- true
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
					log.Debug().Msgf("Tunnel process failed to setup admin server: %s, attempting to restart..", line)

					// Reset the webserver port in case the port is in use
					frpTm.configBuilder.SetAdminPort()
					frpTm.Restart()

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

	<-ackChan

	return nil
}

func (frpTm *FrpTunnelManager) Stop() error {
	return frpTm.clientProcess.Process.Kill()
}

func (frpTm *FrpTunnelManager) Reset() error {
	frpTm.configBuilder.Reset()

	return frpTm.Reload()
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

func NewFrpTunnelManager(messenger messenger.Messenger, config *config.Config) (FrpTunnelManager, error) {

	configBuilder := NewTunnelConfigBuilder(config)
	frpTunnelManager := FrpTunnelManager{
		configBuilder:       configBuilder,
		messenger:           messenger,
		config:              config,
		tunnelsLock:         &sync.RWMutex{},
		tunnelUpdateChan:    make(chan TunnelUpdate),
		activeTunnelConfigs: make(map[string]*Tunnel),
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
		if strings.Contains(string(output), "connect: connection refused") {
			safe.Go(func() {
				frpTm.Restart()
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
		for _, tunnelStatus := range tunnelStatuses {
			tunnelID := CreateTunnelID(tunnelConfig.Subdomain, string(tunnelConfig.Protocol))
			if tunnelID != tunnelStatus.Name {
				continue
			}

			tunnelState := TunnelState{
				Status:       &tunnelStatus,
				Port:         tunnelConfig.LocalPort,
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
	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	out, err := exec.CommandContext(ctx, frpcPath, "status", "-c", frpTm.configBuilder.ConfigPath, "--json").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "connect: connection refused") {
			safe.Go(func() {
				frpTm.Restart()
			})
			return []TunnelStatus{}, nil

		}
		return []TunnelStatus{}, nil
	}

	return parseProxyStatus(string(out))
}

func (frpTm *FrpTunnelManager) Status(tunnelID string) (TunnelStatus, error) {
	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	out, err := exec.CommandContext(ctx, frpcPath, "status", "-c", frpTm.configBuilder.ConfigPath).Output()
	if err != nil {
		log.Error().Err(err).Msg("Error while getting tunnel status")
		return TunnelStatus{}, nil
	}

	tunnelStatuses, err := parseProxyStatus(string(out))
	if err != nil {
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

	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] != nil {
		delete(frpTm.activeTunnelConfigs, tunnelId)
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		log.Warn().Str("tunnelId", tunnelId).Msg("Tunnel does not exist, cannot remove")
		return errors.New("tunnel does not exist")
	}

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
