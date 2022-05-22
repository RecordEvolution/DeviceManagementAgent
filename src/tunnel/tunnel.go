package tunnel

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/go-cmd/cmd"
	"github.com/rs/zerolog/log"
)

type PgrokManager struct {
	TunnelManager
	binaryPath  string
	authToken   string
	serverAddr  string
	tunnels     map[string]*Tunnel
	tunnelsLock sync.Mutex
}

type FullURL struct {
	httpURL  string
	httpsURL string
}

type Tunnel struct {
	process   *cmd.Cmd
	port      string
	subdomain string
	fullURL   FullURL
}

type TunnelManager interface {
	Get(port string) (*Tunnel, error)
	Kill(port string) error
	KillAll() error
	Spawn(port string, subdomain string) (*Tunnel, error)
	Restart(port string) (*Tunnel, error)
}

func NewPgrokTunnel(authToken string, serverAddr string) PgrokManager {
	return PgrokManager{
		authToken:  authToken,
		binaryPath: "/Users/ruben/Desktop/Apps/pgrok/pgrok",
		serverAddr: serverAddr,
		tunnels:    make(map[string]*Tunnel),
	}
}

func (pm *PgrokManager) Get(port string) (*Tunnel, error) {
	pm.tunnelsLock.Lock()
	tunnel := pm.tunnels[port]
	if tunnel == nil {
		pm.tunnelsLock.Unlock()
		return nil, errors.New("tunnel not found")
	}

	pm.tunnelsLock.Unlock()

	return tunnel, nil
}

func (pm *PgrokManager) Restart(port string) (*Tunnel, error) {
	tunnel, err := pm.Get(port)
	if err != nil {
		return nil, err
	}

	err = pm.Kill(port)
	if err != nil {
		return nil, err
	}

	return pm.Spawn(port, tunnel.subdomain)
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

func (pm *PgrokManager) Kill(port string) error {
	tunnel, err := pm.Get(port)
	if err != nil {
		return err
	}

	if tunnel.process != nil {
		tunnel.process.Stop()
	}

	delete(pm.tunnels, port)

	return nil
}

func (pm *PgrokManager) Spawn(port string, subdomain string) (*Tunnel, error) {
	var args = make([]string, 0)
	if subdomain != "" {
		args = append(args, fmt.Sprintf("-subdomain=%s", subdomain))
	}

	args = append(args, "-log-level", "INFO", "-log", "stdout", "-authtoken", pm.authToken, "-serveraddr", pm.serverAddr, port)

	pgrokTunnelCmd := cmd.NewCmd(pm.binaryPath, args...)
	cmdStatusChan := pgrokTunnelCmd.Start()

	var httpURL string
	var httpsURL string

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
			}

			if httpURL != "" && httpsURL != "" {
				break outerLoop
			}
		}
	}

	go func() {
		<-cmdStatusChan

		log.Debug().Msgf("Tunnel for port %s with URL %s has exited.", port, httpsURL)
		pm.tunnelsLock.Lock()
		delete(pm.tunnels, port)
		pm.tunnelsLock.Unlock()
	}()

	result := &Tunnel{
		port:      port,
		subdomain: subdomain,
		process:   pgrokTunnelCmd,
		fullURL: FullURL{
			httpURL:  httpURL,
			httpsURL: httpsURL,
		},
	}

	pm.tunnelsLock.Lock()
	pm.tunnels[port] = result
	pm.tunnelsLock.Unlock()

	return result, nil
}
