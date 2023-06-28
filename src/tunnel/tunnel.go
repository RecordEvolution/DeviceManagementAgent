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
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/messenger/topics"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Protocol string

const (
	TCP  Protocol = "tcp"
	UDP  Protocol = "udp"
	HTTP Protocol = "http"
)

type FrpTunnelManager struct {
	TunnelManager
	tunnelsLock         *sync.RWMutex
	activeTunnelConfigs map[string]*Tunnel
	clientProcess       *exec.Cmd
	configBuilder       TunnelConfigBuilder
	config              *config.Config
	messenger           messenger.Messenger
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
	AppName               string `json:"app_name"`
	Port                  uint64 `json:"port"`
	Protocol              string `json:"protocol"`
	RemotePort            uint64 `json:"remote_port"`
	RemotePortEnvironment string `json:"remote_port_environment"`
	DeviceKey             uint64 `json:"device_key"`
	AppKey                uint64 `json:"app_key"`
	Public                bool   `json:"public"`
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
	ReservePort(portRule PortForwardRule) (uint64, error)
	AddTunnel(portRule PortForwardRule) (TunnelConfig, error)
	RemoveTunnel(conf TunnelConfig, portRule PortForwardRule) error
	Get(tunnelID string) *Tunnel
	Status(tunnelID string) (TunnelStatus, error)
	Reload() error
	Start() error
}

type TunnelStatus struct {
	Name       string
	Status     string
	LocalAddr  string
	Plugin     string
	RemoteAddr string
	Error      string
	Protocol   Protocol
}

func parseProxyStatus(text string) []TunnelStatus {
	lines := strings.Split(text, "\n")
	lines = lines[1:]
	var proxyStatusList []TunnelStatus

	var currentProtocol string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if line == "TCP" || line == "HTTP" || line == "UDP" {
			currentProtocol = line
			i++
			continue
		}

		fields := strings.Fields(line)
		fieldCnt := len(fields)
		if fieldCnt < 4 {
			continue
		}

		proxyStatus := TunnelStatus{
			Name:       fields[0],
			Status:     fields[1],
			LocalAddr:  fields[2],
			RemoteAddr: fields[3],
			Protocol:   Protocol(strings.ToLower(currentProtocol)),
		}

		if fieldCnt == 5 {
			proxyStatus.Error = fields[4]
		}

		proxyStatusList = append(proxyStatusList, proxyStatus)
	}

	return proxyStatusList
}

func (frpTm *FrpTunnelManager) Start() error {
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

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			// Error was found
			if strings.Contains(line, "[E]") {
				tunnelIdRegexp := regexp.MustCompile(`\[(([^\]]+)-(http|tcp|udp))]`)
				tunnelIdMatch := tunnelIdRegexp.FindStringSubmatch(line)
				if len(tunnelIdMatch) > 1 {
					tunnelID := tunnelIdMatch[1]

					errMessageRegexp := regexp.MustCompile(`error: (.*)`)
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
				log.Info().Msgf("frp-out: %s\n", line)
			}
		}
	}()

	return nil
}

func NewFrpTunnelManager(messenger messenger.Messenger, config *config.Config) (FrpTunnelManager, error) {

	configBuilder := NewTunnelConfigBuilder(config)

	frpTunnelManager := FrpTunnelManager{
		configBuilder:       configBuilder,
		messenger:           messenger,
		config:              config,
		tunnelsLock:         &sync.RWMutex{},
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

	reloadCmd := exec.Command(frpcPath, "reload", "-c", frpTm.configBuilder.ConfigPath)
	err := reloadCmd.Start()
	if err != nil {
		return err
	}

	err = reloadCmd.Wait()
	if err != nil {
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

func (frpTm *FrpTunnelManager) Status(tunnelID string) (TunnelStatus, error) {
	frpcPath := filesystem.GetTunnelBinaryPath(frpTm.config, "frpc")

	out, err := exec.Command(frpcPath, "status", "-c", frpTm.configBuilder.ConfigPath).Output()
	if err != nil {
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

func (frpTm *FrpTunnelManager) ReservePort(portRule PortForwardRule) (uint64, error) {
	subdomain := CreateSubdomain(portRule.DeviceKey, portRule.AppName, portRule.Port)

	protocol := Protocol(portRule.Protocol)
	newTunnelConfig := TunnelConfig{Subdomain: subdomain, Protocol: protocol, LocalPort: portRule.Port, RemotePort: portRule.RemotePort}

	// Don't need to reserve a port if the user starts an HTTP tunnel
	if protocol != HTTP {
		log.Debug().Msgf("Reserving port: %d (%s)\n", portRule.Port, portRule.Protocol)
		// If no remote port is set, we will allocate one
		remotePort, err := frpTm.reserveRemotePort(portRule.RemotePort, protocol)
		if err != nil {
			return 0, err
		}
		newTunnelConfig.RemotePort = remotePort
	}

	var url string
	if protocol == HTTP {
		url = fmt.Sprintf("https://%s.%s", subdomain, frpTm.configBuilder.BaseTunnelURL)
	} else {
		url = fmt.Sprintf("%s://%s.%s:%d", strings.ToLower(string(newTunnelConfig.Protocol)), subdomain, frpTm.configBuilder.BaseTunnelURL, newTunnelConfig.RemotePort)
	}

	payload := common.Dict{
		"device_key":  portRule.DeviceKey,
		"app_key":     portRule.AppKey,
		"url":         url,
		"local_port":  newTunnelConfig.LocalPort,
		"remote_port": newTunnelConfig.RemotePort,
		"protocol":    newTunnelConfig.Protocol,
		"active":      true,
		"error":       common.Dict{"message": ""},
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	_, err := frpTm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return newTunnelConfig.RemotePort, err
	}

	return newTunnelConfig.RemotePort, nil
}

// Tries to reserve an external port for kubernetes, updates the frpc client config and reloads the config file
func (frpTm *FrpTunnelManager) AddTunnel(portRule PortForwardRule) (TunnelConfig, error) {
	subdomain := CreateSubdomain(portRule.DeviceKey, portRule.AppName, portRule.Port)

	protocol := Protocol(portRule.Protocol)
	newTunnelConfig := TunnelConfig{Subdomain: subdomain, Protocol: protocol, LocalPort: portRule.Port, RemotePort: portRule.RemotePort}

	// Don't need to reserve a port if the user starts an HTTP tunnel
	if protocol != HTTP {
		// If no remote port is set, we will allocate one
		remotePort, err := frpTm.reserveRemotePort(portRule.RemotePort, protocol)
		if err != nil {
			return TunnelConfig{}, err
		}
		newTunnelConfig.RemotePort = remotePort
	}

	frpTm.configBuilder.AddTunnelConfig(newTunnelConfig)

	err := frpTm.Reload()
	if err != nil {
		return TunnelConfig{}, err
	}

	var url string
	if protocol == HTTP {
		url = fmt.Sprintf("https://%s.%s", subdomain, frpTm.configBuilder.BaseTunnelURL)
	} else {
		url = fmt.Sprintf("%s://%s.%s:%d", strings.ToLower(string(newTunnelConfig.Protocol)), subdomain, frpTm.configBuilder.BaseTunnelURL, newTunnelConfig.RemotePort)
	}

	payload := common.Dict{
		"device_key":  portRule.DeviceKey,
		"app_key":     portRule.AppKey,
		"url":         url,
		"local_port":  newTunnelConfig.LocalPort,
		"remote_port": newTunnelConfig.RemotePort,
		"protocol":    newTunnelConfig.Protocol,
		"active":      true,
		"error":       common.Dict{"message": ""},
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	tunnelId := CreateTunnelID(subdomain, portRule.Protocol)
	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] == nil {
		frpTm.activeTunnelConfigs[tunnelId] = &Tunnel{Config: newTunnelConfig}
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return TunnelConfig{}, errors.New("tunnel already exists")
	}

	_, err = frpTm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return TunnelConfig{}, nil
	}

	return newTunnelConfig, nil
}

func (frpTm *FrpTunnelManager) RemoveTunnel(conf TunnelConfig, portRule PortForwardRule) error {
	tunnelId := CreateTunnelID(conf.Subdomain, string(conf.Protocol))

	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] != nil {
		delete(frpTm.activeTunnelConfigs, tunnelId)
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return errors.New("tunnel does not exist")
	}

	// The port manager will automatically cleanup unused ports
	// err := frpTm.closeRemotePort(uint16(conf.RemotePort), conf.Protocol)
	// if err != nil {
	// 	return err
	// }

	frpTm.configBuilder.RemoveTunnelConfig(conf)

	frpTm.Reload()

	payload := common.Dict{
		"device_key":  portRule.DeviceKey,
		"app_key":     portRule.AppKey,
		"active":      false,
		"url":         "", // remove url in database
		"local_port":  conf.LocalPort,
		"remote_port": conf.RemotePort,
		"protocol":    conf.Protocol,
		"error":       common.Dict{"message": ""},
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	_, err := frpTm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}
