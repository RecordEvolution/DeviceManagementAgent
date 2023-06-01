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

type PgrokManager struct {
	TunnelManager
	Config      *config.Config
	binaryPath  string
	serverAddr  string
	tunnels     map[uint64]*Tunnel
	tunnelsLock sync.Mutex
}

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
	Main      bool   `json:"main"`
	RuleName  string `json:"name"`
	AppName   string `json:"app_name"`
	Port      uint64 `json:"port"`
	Protocol  string `json:"protocol"`
	DeviceKey uint64 `json:"device_key"`
	AppKey    uint64 `json:"app_key"`
	Public    bool   `json:"public"`
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

		portEntries = append(portEntries, portEntry)

	}

	return portEntries, nil
}

type TunnelManager interface {
	AddTunnel(portRule PortForwardRule) (TunnelConfig, error)
	RemoveTunnel(conf TunnelConfig, portRule PortForwardRule) error
	Get(tunnelID string) *Tunnel
	Status(tunnelID string) (TunnelStatus, error)
	Reload() error
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

func NewFrpTunnelManager(messenger messenger.Messenger, config *config.Config) (FrpTunnelManager, error) {
	frpcPath := filesystem.GetTunnelBinaryPath(config, "frpc")

	configBuilder := NewTunnelConfigBuilder(config)
	frpCommand := exec.Command(frpcPath)
	frpCommand.Dir = filepath.Dir(frpcPath)

	stdout, err := frpCommand.StdoutPipe()
	if err != nil {
		return FrpTunnelManager{}, err
	}

	err = frpCommand.Start()
	if err != nil {
		return FrpTunnelManager{}, err
	}

	frpTunnelManager := FrpTunnelManager{
		clientProcess:       frpCommand,
		configBuilder:       configBuilder,
		messenger:           messenger,
		config:              config,
		tunnelsLock:         &sync.RWMutex{},
		activeTunnelConfigs: make(map[string]*Tunnel),
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			// Error was found
			if strings.Contains(line, "[E]") {
				tunnelIdRegexp := regexp.MustCompile(`\[(([^\]]+)-(http|tcp|udp)-(\d*))]`)
				tunnelIdMatch := tunnelIdRegexp.FindStringSubmatch(line)
				if len(tunnelIdMatch) > 1 {
					tunnelID := tunnelIdMatch[1]

					errMessageRegexp := regexp.MustCompile(`error: (.*)`)
					errMatch := errMessageRegexp.FindStringSubmatch(line)
					if len(errMatch) > 1 {
						errMessage := errMatch[1]
						frpTunnelManager.tunnelsLock.Lock()
						activeTunnel := frpTunnelManager.activeTunnelConfigs[tunnelID]
						activeTunnel.Error = errMessage
						log.Debug().Msgf("%+v\n", activeTunnel)
						frpTunnelManager.tunnelsLock.Unlock()
					}

				}

				log.Error().Msgf("frp-err: %s\n", line)

			} else {
				log.Info().Msgf("frp-out: %s\n", line)
			}
		}
	}()

	return frpTunnelManager, nil
}

func (frpTm *FrpTunnelManager) closeRemotePort(remotePort uint16, protocol Protocol) error {
	args := []interface{}{
		common.Dict{
			"remote_port": remotePort,
			"protocol":    string(protocol),
		},
	}

	_, err := frpTm.messenger.Call(context.Background(), topics.ClosePort, args, common.Dict{}, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (frpTm *FrpTunnelManager) SetMessenger(messenger messenger.Messenger) {
	frpTm.messenger = messenger
}

func (frpTm *FrpTunnelManager) reserveRemotePort(protocol Protocol) (uint64, error) {
	args := []interface{}{
		common.Dict{
			"protocol": string(protocol),
		},
	}

	result, err := frpTm.messenger.Call(context.Background(), topics.ExposePort, args, common.Dict{}, nil, nil)
	if err != nil {
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

// Tries to reserve an external port for kubernetes, updates the frpc client config and reloads the config file
func (frpTm *FrpTunnelManager) AddTunnel(portRule PortForwardRule) (TunnelConfig, error) {
	subdomain := strings.ToLower(fmt.Sprintf("%d_%s_%d", portRule.DeviceKey, portRule.AppName, portRule.Port))

	protocol := Protocol(portRule.Protocol)
	newTunnelConfig := TunnelConfig{Subdomain: subdomain, Protocol: protocol, LocalPort: portRule.Port}

	tunnelId := GetTunnelID(subdomain, portRule.Protocol, portRule.Port)
	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] == nil {
		frpTm.activeTunnelConfigs[tunnelId] = &Tunnel{Config: newTunnelConfig}
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return TunnelConfig{}, errors.New("tunnel already exists")
	}

	// Don't need to reserve a port if the user starts an HTTP tunnel
	if protocol != HTTP {
		remotePort, err := frpTm.reserveRemotePort(protocol)
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
		url = fmt.Sprintf("%s.%s", subdomain, frpTm.configBuilder.BaseTunnelURL)
	} else {
		url = fmt.Sprintf("%s:%s:%d", subdomain, frpTm.configBuilder.BaseTunnelURL, newTunnelConfig.RemotePort)
	}

	payload := common.Dict{
		"device_key": portRule.DeviceKey,
		"app_key":    portRule.AppKey,
		"url":        url,
		"port":       newTunnelConfig.LocalPort,
		"active":     true,
		"error":      common.Dict{"message": ""},
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	_, err = frpTm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return TunnelConfig{}, nil
	}

	return newTunnelConfig, nil
}

func (frpTm *FrpTunnelManager) RemoveTunnel(conf TunnelConfig, portRule PortForwardRule) error {
	tunnelId := GetTunnelID(conf.Subdomain, string(conf.Protocol), conf.LocalPort)

	frpTm.tunnelsLock.Lock()
	if frpTm.activeTunnelConfigs[tunnelId] != nil {
		delete(frpTm.activeTunnelConfigs, tunnelId)
		frpTm.tunnelsLock.Unlock()
	} else {
		frpTm.tunnelsLock.Unlock()
		return errors.New("tunnel does not exist")
	}

	err := frpTm.closeRemotePort(uint16(conf.RemotePort), conf.Protocol)
	if err != nil {
		return err
	}

	frpTm.configBuilder.RemoveTunnelConfig(conf)

	frpTm.Reload()

	payload := common.Dict{
		"device_key": portRule.DeviceKey,
		"app_key":    portRule.AppKey,
		"active":     false,
		"url":        "", // remove url in database
		"port":       conf.RemotePort,
		"error":      common.Dict{"message": ""},
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()

	_, err = frpTm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

type AppTunnelManager interface {
	GetTunnelManager() TunnelManager
	SetMessenger(m messenger.Messenger)
	GetAppTunnel(appKey uint64, port uint64) (*AppTunnel, error)
	CreateAppTunnel(appKey uint64, appName string, deviceKey uint64, port uint64, protocol string) (*AppTunnel, error)
	RegisterAppTunnel(appKey uint64, appName string, deviceKey uint64, port uint64, protocol string) *AppTunnel
	DeactivateAppTunnel(appTunnel *AppTunnel) error
	ActivateAppTunnel(appTunnel *AppTunnel) error
	KillAppTunnel(appKey uint64, port uint64) error
}
