package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/system"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-cmd/cmd"
	"github.com/rs/zerolog/log"
)

const HTTPS = "https"
const HTTP = "http"
const HTTP_HTTPS = "http+https"
const TCP = "tcp"

type PgrokManager struct {
	TunnelManager
	Config      *config.Config
	binaryPath  string
	serverAddr  string
	tunnels     map[uint64]*Tunnel
	tunnelsLock sync.Mutex
}

type FullURL struct {
	HttpURL  string `json:"httpURL"`
	HttpsURL string `json:"httpsURL"`
	TcpURL   string `json:"tcpURL"`
}

type Tunnel struct {
	process   *cmd.Cmd
	Port      uint64  `json:"port"`
	Subdomain string  `json:"subdomain"`
	Protocol  string  `json:"protocol"`
	FullURL   FullURL `json:"fullURL"`
}

type AppTunnel struct {
	Tunnel    *Tunnel
	Name      string
	DeviceKey uint64
	AppKey    uint64
	Main      bool
	Public    bool
	Running   bool
}

type PortForwardRule struct {
	Main      bool   `json:"main"`
	Name      string `json:"name"`
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
	Get(port uint64) (*Tunnel, error)
	GetAll() ([]*Tunnel, error)
	Kill(port uint64) error
	KillAll() error
	Spawn(port uint64, protocol string, subdomain string) (*Tunnel, error)
	Restart(port uint64) (*Tunnel, error)
}

type AppTunnelManager interface {
	GetTunnelManager() TunnelManager
	SetMessenger(m messenger.Messenger)
	GetAppTunnel(appKey uint64) (*AppTunnel, error)
	CreateAppTunnel(appKey uint64, deviceKey uint64, port uint64, protocol string, subdomain string) (*AppTunnel, error)
	RegisterAppTunnel(appKey uint64, deviceKey uint64, port uint64, protocol string, subdomain string) *AppTunnel
	DeactivateAppTunnel(appTunnel *AppTunnel) error
	ActivateAppTunnel(appTunnel *AppTunnel) error
	KillAppTunnel(appKey uint64) error
}

func NewPgrokAppTunnelManager(tm TunnelManager, m messenger.Messenger) PgrokAppTunnelManager {
	return PgrokAppTunnelManager{
		TunnelManager: tm,
		messenger:     m,
		appTunnels:    make(map[uint64]*AppTunnel),
	}
}

func (pm *PgrokAppTunnelManager) GetTunnelManager() TunnelManager {
	return pm.TunnelManager
}

type PgrokAppTunnelManager struct {
	AppTunnelManager
	messenger      messenger.Messenger
	TunnelManager  TunnelManager
	appTunnels     map[uint64]*AppTunnel
	appTunnelsLock sync.Mutex
}

func (pm *PgrokAppTunnelManager) DeactivateAppTunnel(appTunnel *AppTunnel) error {
	if !appTunnel.Running {
		return errors.New("tunnel is not running")
	}

	foundAppTunnel, _ := pm.GetAppTunnel(appTunnel.AppKey)
	if foundAppTunnel == nil {
		return errors.New("tunnel does not exist")
	}

	payload := common.Dict{
		"device_key": appTunnel.DeviceKey,
		"app_key":    appTunnel.AppKey,
		"url":        "", // remove url in database
		"port":       appTunnel.Tunnel.Port,
	}

	err := pm.TunnelManager.Kill(appTunnel.Tunnel.Port)
	if err != nil {
		log.Error().Err(err).Msgf("failed to kill tunnel on app tunnel")
	}

	appTunnel.Running = false

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)
	defer cancelFunc()

	fmt.Printf("%+v\n", payload)
	_, err = pm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (pm *PgrokAppTunnelManager) ActivateAppTunnel(appTunnel *AppTunnel) error {
	if appTunnel.Running {
		return errors.New("tunnel is already running")
	}

	foundAppTunnel, _ := pm.GetAppTunnel(appTunnel.AppKey)
	if foundAppTunnel == nil {
		pm.appTunnels[appTunnel.AppKey] = appTunnel
	}

	tunnel, err := pm.TunnelManager.Spawn(appTunnel.Tunnel.Port, appTunnel.Tunnel.Protocol, appTunnel.Tunnel.Subdomain)
	if err != nil {
		return err
	}

	appTunnel.Tunnel = tunnel
	appTunnel.Running = true

	url := tunnel.FullURL.HttpsURL
	if tunnel.Protocol == TCP {
		url = tunnel.FullURL.TcpURL
	}

	payload := common.Dict{
		"device_key": appTunnel.DeviceKey,
		"app_key":    appTunnel.AppKey,
		"url":        url,
		"port":       tunnel.Port,
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)
	defer cancelFunc()

	_, err = pm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (pm *PgrokAppTunnelManager) RegisterAppTunnel(appKey uint64, deviceKey uint64, port uint64, protocol string, subdomain string) *AppTunnel {
	appTunnel := AppTunnel{
		Tunnel: &Tunnel{
			Port:      port,
			Subdomain: subdomain,
			Protocol:  protocol,
		},
		DeviceKey: deviceKey,
		AppKey:    appKey,
		Running:   false,
	}

	pm.appTunnelsLock.Lock()
	pm.appTunnels[appKey] = &appTunnel
	pm.appTunnelsLock.Unlock()

	return &appTunnel
}

func (pm *PgrokAppTunnelManager) CreateAppTunnel(appKey uint64, deviceKey uint64, port uint64, protocol string, subdomain string) (*AppTunnel, error) {
	tunnel, err := pm.TunnelManager.Spawn(port, protocol, subdomain)
	if err != nil {
		return nil, err
	}

	appTunnel := AppTunnel{
		Tunnel:    tunnel,
		DeviceKey: deviceKey,
		AppKey:    appKey,
		Running:   true,
	}

	pm.appTunnelsLock.Lock()
	pm.appTunnels[appKey] = &appTunnel
	pm.appTunnelsLock.Unlock()

	url := tunnel.FullURL.HttpsURL
	if tunnel.Protocol == TCP {
		url = tunnel.FullURL.TcpURL
	}

	payload := common.Dict{
		"device_key": deviceKey,
		"app_key":    appKey,
		"url":        url,
		"port":       port,
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)
	defer cancelFunc()

	_, err = pm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	return &appTunnel, nil
}

func (pm *PgrokAppTunnelManager) GetAppTunnel(appKey uint64) (*AppTunnel, error) {
	pm.appTunnelsLock.Lock()
	appTunnel := pm.appTunnels[appKey]
	pm.appTunnelsLock.Unlock()

	if appTunnel == nil {
		return nil, errors.New("app tunnel not found")
	}

	return appTunnel, nil
}

func (pm *PgrokAppTunnelManager) KillAppTunnel(appKey uint64) error {
	pm.appTunnelsLock.Lock()
	appTunnel := pm.appTunnels[appKey]
	pm.appTunnelsLock.Unlock()

	if appTunnel == nil {
		return errors.New("app tunnel not found")
	}

	payload := common.Dict{
		"device_key": appTunnel.DeviceKey,
		"app_key":    appTunnel.AppKey,
		"url":        "", // remove url in database
		"port":       appTunnel.Tunnel.Port,
	}

	err := pm.TunnelManager.Kill(appTunnel.Tunnel.Port)
	if err != nil {
		log.Error().Err(err).Msg("Failed to kill tunnel of app tunnel")
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)
	defer cancelFunc()

	_, err = pm.messenger.Call(ctx, topics.UpdateAppTunnel, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	pm.appTunnelsLock.Lock()
	delete(pm.appTunnels, appKey)
	pm.appTunnelsLock.Unlock()

	return nil
}

func (pm *PgrokAppTunnelManager) SetMessenger(m messenger.Messenger) {
	pm.messenger = m
}

func getPgrokBinaryPath(config *config.Config) string {
	binaryName := "pgrok"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	return filepath.Join(config.CommandLineArguments.AgentDir, binaryName)
}

func NewPgrokTunnel(config *config.Config) PgrokManager {
	env := system.GetEnvironment(config)
	serverAddr := "app.datapods.io:4443"
	if env == "production" {
		serverAddr = "app.record-evolution.com:4443"
	}

	binaryPath := getPgrokBinaryPath(config)
	return PgrokManager{
		Config:     config,
		binaryPath: binaryPath,
		serverAddr: serverAddr,
		tunnels:    make(map[uint64]*Tunnel),
	}
}

func (pm *PgrokManager) Get(port uint64) (*Tunnel, error) {
	pm.tunnelsLock.Lock()
	tunnel := pm.tunnels[port]
	if tunnel == nil {
		pm.tunnelsLock.Unlock()
		return nil, errors.New("tunnel not found")
	}

	pm.tunnelsLock.Unlock()

	return tunnel, nil
}

func (pm *PgrokManager) GetAll() ([]*Tunnel, error) {
	var tunnels []*Tunnel

	pm.tunnelsLock.Lock()
	for _, tunnel := range pm.tunnels {
		tunnels = append(tunnels, tunnel)
	}

	pm.tunnelsLock.Unlock()

	return tunnels, nil
}

func (pm *PgrokManager) Restart(port uint64) (*Tunnel, error) {
	tunnel, err := pm.Get(port)
	if err != nil {
		return nil, err
	}

	err = pm.Kill(port)
	if err != nil {
		return nil, err
	}

	return pm.Spawn(port, tunnel.Subdomain, tunnel.Protocol)
}

func (pm *PgrokManager) KillAll() error {
	for port := range pm.tunnels {
		err := pm.Kill(port)
		if err != nil {
			return err
		}
	}
	return nil
}

func (pm *PgrokManager) Kill(port uint64) error {
	tunnel, err := pm.Get(port)
	if err != nil {
		return err
	}

	if tunnel.process != nil {
		tunnel.process.Stop()
	}

	delete(pm.tunnels, port)

	log.Debug().Msgf("Killed pgrok tunnel with port %d", port)

	return nil
}

func (pm *PgrokManager) Spawn(port uint64, protocol string, subdomain string) (*Tunnel, error) {

	pm.tunnelsLock.Lock()
	tunnel := pm.tunnels[port]
	if tunnel != nil {
		pm.tunnelsLock.Unlock()
		return nil, fmt.Errorf("a tunnel already exists for port %d", port)
	}

	pm.tunnelsLock.Unlock()

	var args = make([]string, 0)
	if subdomain != "" {
		args = append(args, fmt.Sprintf("-subdomain=%s", subdomain))
	}

	if protocol != TCP {
		protocol = HTTP_HTTPS
	}

	deviceKey := pm.Config.ReswarmConfig.DeviceKey
	deviceSecret := pm.Config.ReswarmConfig.Secret

	auth := fmt.Sprintf("%d:%s", deviceKey, deviceSecret)
	args = append(args, "-log", "stdout", "-auth", auth, "-serveraddr", pm.serverAddr, "-proto", protocol, fmt.Sprint(port))
	pgrokTunnelCmd := cmd.NewCmd(pm.binaryPath, args...)
	cmdStatusChan := pgrokTunnelCmd.Start()

	var httpURL string
	var httpsURL string
	var tcpURL string

outerLoop:
	for {
		status := pgrokTunnelCmd.Status()
		for _, val := range status.Stdout {
			if strings.Contains(val, "control recovering from failure dial") {
				return &Tunnel{}, errors.New("failed to establish connection")
			}

			if strings.Contains(val, "is already registered") {
				return &Tunnel{}, errors.New("subdomain already exists")
			}

			if strings.Contains(val, "Tunnel established at") {
				if protocol != TCP {
					httpsURLRegex := regexp.MustCompile(`Tunnel established at (https.*)`)
					httpsMatch := httpsURLRegex.FindStringSubmatch(val)
					if len(httpsMatch) == 2 && httpsMatch[1] != "" {
						httpsURL = httpsMatch[1]
					}

					httpURLRegex := regexp.MustCompile(`Tunnel established at (http:.*)`)
					httpMatch := httpURLRegex.FindStringSubmatch(val)
					if len(httpMatch) == 2 && httpMatch[1] != "" {
						httpURL = httpMatch[1]
					}
				} else {
					tcpURLRegex := regexp.MustCompile(`Tunnel established at (tcp:.*)`)
					tcpMatch := tcpURLRegex.FindStringSubmatch(val)
					if len(tcpMatch) == 2 && tcpMatch[1] != "" {
						tcpURL = tcpMatch[1]
					}
				}
			}

			if protocol == TCP {
				if tcpURL != "" {
					break outerLoop
				}
			} else {
				if httpURL != "" && httpsURL != "" {
					break outerLoop
				}
			}

			time.Sleep(time.Millisecond * 10)
		}
	}

	log.Debug().Msgf("Started Pgrok tunnel on port %d. (https: %s, http: %s, tcp: %s)", port, httpsURL, httpURL, tcpURL)

	go func() {
		<-cmdStatusChan

		log.Debug().Msgf("Tunnel for port %d with URL %s has exited.", port, httpsURL)
		pm.tunnelsLock.Lock()
		delete(pm.tunnels, port)
		pm.tunnelsLock.Unlock()
	}()

	result := &Tunnel{
		Port:      port,
		Subdomain: subdomain,
		Protocol:  protocol,
		process:   pgrokTunnelCmd,
		FullURL: FullURL{
			HttpURL:  httpURL,
			HttpsURL: httpsURL,
			TcpURL:   tcpURL,
		},
	}

	pm.tunnelsLock.Lock()
	pm.tunnels[port] = result
	pm.tunnelsLock.Unlock()

	return result, nil
}
