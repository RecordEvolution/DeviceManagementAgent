package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/debounce"
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

type PortForwardRule struct {
	Main                  bool   `json:"main"`
	RuleName              string `json:"name"`
	Active                bool   `json:"active"`
	Port                  uint64 `json:"port"`
	Protocol              string `json:"protocol"`
	LocalIP               string `json:"local_ip"`
	RemotePortEnvironment string `json:"remote_port_environment"`
}

func InterfaceToPortForwardRule(dat []interface{}) ([]PortForwardRule, error) {
	portEntries := make([]PortForwardRule, 0, len(dat))

	for _, portEntry := range dat {
		jsonStr, err := json.Marshal(portEntry)
		if err != nil {
			return nil, err
		}

		var portEntry PortForwardRule
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

type TunnelState struct {
	Status  *TunnelStatus `json:"status"`
	AppName string        `json:"app_name"`
	Port    uint64        `json:"port"`
	Active  bool          `json:"active"`
	URL     string        `json:"url"`
}

func parseProxyStatus(text string) []TunnelStatus {
	lines := strings.Split(text, "\n")
	lines = lines[1:]
	var proxyStatusList []TunnelStatus

	var currentProtocol string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if line == "TCP" || line == "HTTP" || line == "HTTPS" || line == "UDP" {
			currentProtocol = line
			i++
			continue
		}

		fields := strings.Fields(line)
		fieldCnt := len(fields)
		if fieldCnt < 4 {
			continue
		}

		remoteAddr := fields[3]
		remoteAddrSplit := strings.Split(remoteAddr, ":")

		var remotePortStr string
		var remotePort int64

		if len(remoteAddrSplit) == 2 {
			remotePortStr = remoteAddrSplit[1]
			remotePort, _ = strconv.ParseInt(remotePortStr, 10, 64)
		}

		// TODO: read full status on failed to start ports
		proxyStatus := TunnelStatus{
			Name:       fields[0],
			Status:     fields[1],
			LocalAddr:  fields[2],
			RemoteAddr: fields[3],
			RemotePort: uint64(remotePort),
			Protocol:   Protocol(strings.ToLower(currentProtocol)),
		}

		if fieldCnt > 4 {
			proxyStatus.Error = strings.Join(fields[4:], " ")
		}

		proxyStatusList = append(proxyStatusList, proxyStatus)
	}

	return proxyStatusList
}

var tunnelIdRegexp = regexp.MustCompile(`\[(([^\]]+)-(http|https|tcp|udp))]`)
var errMessageRegexp = regexp.MustCompile(`error: (.*)`)
var proxyNameRegex = regexp.MustCompile(`\[(\d+)-(.*)-(\d+)-(.*)\]`)

func (frpTm *FrpTunnelManager) Restart() error {
	if frpTm.clientProcess == nil || frpTm.clientProcess.Process == nil {
		return errors.New("frp client is not running")
	}

	err := frpTm.clientProcess.Process.Kill()
	if err != nil {
		return err
	}

	processState, err := frpTm.clientProcess.Process.Wait()
	if err != nil {
		return err
	}

	log.Debug().Msgf("Tunnel client process exited with state: %+v\n", processState)

	time.Sleep(time.Second * 5)

	return frpTm.Start()
}

func (frpTm *FrpTunnelManager) Start() error {
	log.Debug().Msg("Starting tunnel client")

	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	frpCommand := exec.Command(frpcPath)
	frpCommand.Dir = filepath.Dir(frpcPath)

	frpTm.clientProcess = frpCommand

	stdout, err := frpCommand.StdoutPipe()
	if err != nil {
		return err
	}

	err = frpCommand.Start()
	if err != nil {
		return err
	}

	ackChan := make(chan bool)
	go func() {
		scanner := bufio.NewScanner(stdout)
		debounced := debounce.New(100 * time.Millisecond)

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
							log.Error().Msgf("failed to get tunnel with ID %s\n", tunnelID)
						}
						frpTm.tunnelsLock.Unlock()
					}

				}

				log.Error().Msgf("frp-err: %s\n", line)

			} else {
				proxyStarted := strings.Contains(line, "start proxy success")
				proxyRemoved := strings.Contains(line, "proxy removed")
				runAdminServerError := strings.Contains(line, "run admin server error")

				if runAdminServerError {
					log.Debug().Msg("Tunnel process failed to setup admin server, attempting to restart..")
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

					debounced(func() {
						updateTopic := common.BuildTunnelStateUpdate(frpTm.config.ReswarmConfig.SerialNumber)
						tunnelStates, err := frpTm.GetState()
						if err != nil {
							return
						}

						var args []interface{}
						for _, tunnelState := range tunnelStates {
							args = append(args, tunnelState)
						}

						frpTm.messenger.Publish(topics.Topic(updateTopic), args, nil, nil)
					})
				}

				log.Info().Msgf("frp-out: %s\n", line)
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
		return nil, err
	}

	tunnelStatuses, err := frpTm.AllStatus()
	if err != nil {
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
				Status:  &tunnelStatus,
				Port:    tunnelConfig.LocalPort,
				AppName: tunnelConfig.AppName,
				Active:  tunnelStatus.Status == "running",
				URL:     frpTm.buildURL(tunnelConfig.Protocol, tunnelConfig.Subdomain, tunnelStatus.RemotePort),
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

	out, err := exec.CommandContext(ctx, frpcPath, "status", "-c", frpTm.configBuilder.ConfigPath).Output()
	if err != nil {
		log.Error().Err(err).Msg("Error while getting tunnel status")
		return []TunnelStatus{}, nil
	}

	return parseProxyStatus(string(out)), nil
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

	tunnelStatuses := parseProxyStatus(string(out))
	for _, tunnelStatus := range tunnelStatuses {
		if tunnelStatus.Name == tunnelID {
			return tunnelStatus, nil
		}
	}

	return TunnelStatus{}, errdefs.ErrNotFound
}

// Tries to reserve an external port for kubernetes, updates the frpc client config and reloads the config file
func (frpTm *FrpTunnelManager) AddTunnel(config TunnelConfig) (TunnelConfig, error) {
	// Don't need to reserve a port if the user starts an HTTP tunnel
	if config.Protocol != HTTP && config.Protocol != HTTPS {
		// If no remote port is set, we will allocate one
		remotePort, err := frpTm.reserveRemotePort(config.RemotePort, config.Protocol)
		if err != nil {
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
	tunnelId := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] != nil {
		delete(frpTm.activeTunnelConfigs, tunnelId)
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return errors.New("tunnel does not exist")
	}

	frpTm.configBuilder.RemoveTunnelConfig(conf)

	err := frpTm.Reload()
	if err != nil {
		return err
	}

	// for update := range frpTm.tunnelUpdateChan {
	// 	if update.AppName == strings.ToLower(conf.AppName) &&
	// 		update.LocalPort == conf.LocalPort &&
	// 		update.Protocol == conf.Protocol &&
	// 		update.UpdateType == REMOVED {
	// 		break
	// 	}
	// }

	return nil
}
