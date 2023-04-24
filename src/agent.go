package main

import (
	"context"
	"fmt"
	"reagent/api"
	"reagent/apps"
	"reagent/benchmark"
	"reagent/common"
	"reagent/config"
	"reagent/container"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/network"
	"reagent/persistence"
	"reagent/privilege"
	"reagent/release"
	"reagent/safe"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"
	"reagent/tunnel"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Agent struct {
	Container       container.Container
	Messenger       messenger.Messenger
	LogMessenger    messenger.Messenger
	Database        persistence.Database
	Network         network.Network
	System          *system.System
	Config          *config.Config
	External        *api.External
	LogManager      *logging.LogManager
	TerminalManager *terminal.TerminalManager
	TunnelManager   tunnel.AppTunnelManager
	Filesystem      *filesystem.Filesystem
	AppManager      *apps.AppManager
	StateObserver   *apps.StateObserver
	StateMachine    *apps.StateMachine
}

func (agent *Agent) OnConnect() error {
	var wg sync.WaitGroup

	err := agent.Messenger.UpdateRemoteDeviceStatus(messenger.CONFIGURING)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	wg.Add(1)
	safe.Go(func() {
		defer wg.Done()

		_, err := agent.System.UpdateSystem(nil, agent.Config.CommandLineArguments.ShouldUpdateAgent)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to update system")
		}
	})

	err = agent.System.UpdateDeviceMetadata()
	if err != nil {
		log.Error().Err(err).Msgf("update device metadata")
	}

	err = agent.updateRemoteDevice()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device metadata")
	}

	// first call this in case we don't have any app state yet, then we can start containers accordingly
	err = agent.AppManager.UpdateLocalRequestedAppStatesWithRemote()
	if err != nil {
		if strings.Contains(err.Error(), "tunnel") {
			log.Error().Stack().Err(err).Msg("failed to sync")
		} else {
			log.Fatal().Stack().Err(err).Msg("failed to sync")
		}
	}

	err = agent.StateObserver.CorrectAppStates(true)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to CorrectLocalAndUpdateRemoteAppStates")
	}

	err = agent.StateObserver.ObserveAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init app state observers")
	}

	err = agent.AppManager.EnsureRemoteRequestedStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to EvaluateRequestedStates")
	}

	wg.Add(1)
	safe.Go(func() {
		defer wg.Done()

		err = agent.LogManager.ReviveDeadLogs()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to revive dead logs")
		}

		err := agent.LogManager.SetupEndpoints()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup endpoints")
		}
	})

	log.Debug().Msg("Waiting for startup setup to finish...")
	wg.Wait()

	log.Debug().Msg("Registering all endpoints...")
	err = agent.External.RegisterAll()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to register all external endpoints")
	}

	log.Debug().Msg("Startup setup finished!")
	err = agent.Messenger.UpdateRemoteDeviceStatus(messenger.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	benchmark.TimeTillGreen = time.Since(benchmark.GreenInit)

	return err
}

func (agent *Agent) InitConnectionStatusHeartbeat() {
	safe.Go(func() {
		for {
			time.Sleep(time.Second * 30)
			agent.Messenger.UpdateRemoteDeviceStatus(messenger.CONNECTED)
		}
	})
}

func (agent *Agent) updateRemoteDevice() error {
	config := agent.Config
	ctx := context.Background()

	_, arch, variant := release.GetSystemInfo()
	payload := common.Dict{
		"swarm_key":    config.ReswarmConfig.SwarmKey,
		"device_key":   config.ReswarmConfig.DeviceKey,
		"architecture": arch + variant,
	}

	_, err := agent.Messenger.Call(ctx, topics.UpdateDeviceArchitecture, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func NewAgent(generalConfig *config.Config) (agent *Agent) {
	cliArgs := generalConfig.CommandLineArguments

	database, err := persistence.NewSQLiteDb(generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to create SQLite db instance")
	}

	err = database.Init()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to initalize SQLite database")
	}

	// setup the agent struct with a dummy/offline implementation of the messenger
	dummyMessenger := messenger.NewOffline(generalConfig)
	container, err := container.NewDocker(generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup docker")
	}

	filesystem := filesystem.New()
	tunnelManager := tunnel.NewPgrokTunnel(generalConfig)
	appTunnelManager := tunnel.NewPgrokAppTunnelManager(&tunnelManager, dummyMessenger)
	appStore := store.NewAppStore(database, dummyMessenger)
	logManager := logging.NewLogManager(container, dummyMessenger, database, appStore)
	stateObserver := apps.NewObserver(container, &appStore, &logManager)
	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver, &filesystem)
	appManager := apps.NewAppManager(&stateMachine, &appStore, &stateObserver, &appTunnelManager)
	terminalManager := terminal.NewTerminalManager(dummyMessenger, container)

	var networkInstance network.Network
	if runtime.GOOS == "linux" && cliArgs.UseNetworkManager {
		networkInstance, err = network.NewNMWNetwork()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup network")
		}
	} else {
		// TODO: write implementations for other environments. (issue: https://github.com/RecordEvolution/DeviceManagementAgent/issues/41)
		networkInstance = network.NewDummyNetwork()
	}

	err = stateObserver.CorrectAppStates(false)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to correct local states")
	}

	err = stateObserver.ObserveAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init app state observers")
	}

	// setup the containers on start
	err = appManager.EnsureLocalRequestedStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to ensure local app states")
	}

	// try to establish the main session
	mainSocketConfig := messenger.SocketConfig{
		SetupTestament:    true,
		ResponseTimeout:   time.Millisecond * time.Duration(cliArgs.ResponseTimeout),
		PingPongTimeout:   time.Millisecond * time.Duration(cliArgs.PingPongTimeout),
		ConnectionTimeout: time.Millisecond * time.Duration(cliArgs.ConnectionEstablishTimeout),
	}

	benchmark.TimeTillPreConnectInit = time.Since(benchmark.PreConnectInit)

	fmt.Println("Attempting to establish a socket connection...")

	benchmark.OnConnectInitAfterConnection = time.Now()
	benchmark.SocketConnectionInit = time.Now()

	mainSession, err := messenger.NewWamp(generalConfig, &mainSocketConfig, container)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup wamp connection")
	}

	benchmark.TimeTillSocketConnection = time.Since(benchmark.SocketConnectionInit)
	benchmark.TimeTillSocketConnectionFromLaunch = time.Since(benchmark.SocketConnectionInitFromLaunch)

	fmt.Println("Connected!")

	systemAPI := system.New(generalConfig, mainSession)

	// established a connection, replace the dummy messenger
	appStore.SetMessenger(mainSession)
	terminalManager.SetMessenger(mainSession)
	terminalManager.InitUnregisterWatcher()
	logManager.SetMessenger(mainSession)
	appTunnelManager.SetMessenger(mainSession)
	privilege := privilege.NewPrivilege(mainSession, generalConfig)

	external := api.External{
		Container:        container,
		Messenger:        mainSession,
		LogMessenger:     mainSession,
		Database:         database,
		Network:          networkInstance,
		Privilege:        &privilege,
		Filesystem:       &filesystem,
		AppTunnelManager: &appTunnelManager,
		System:           &systemAPI,
		AppManager:       appManager,
		TerminalManager:  &terminalManager,
		LogManager:       &logManager,
		Config:           generalConfig,
	}

	return &Agent{
		Config:          generalConfig,
		System:          &systemAPI,
		External:        &external,
		LogManager:      &logManager,
		Network:         networkInstance,
		TerminalManager: &terminalManager,
		TunnelManager:   &appTunnelManager,
		AppManager:      appManager,
		StateObserver:   &stateObserver,
		StateMachine:    &stateMachine,
		Filesystem:      &filesystem,
		Container:       container,
		Messenger:       mainSession,
		LogMessenger:    mainSession,
		Database:        database,
	}
}

func (agent *Agent) ListenForDisconnect() {
	safe.Go(func() {
	reconnect:
		select {
		case <-agent.Messenger.Done():
			log.Debug().Msg("Received done signal for main session")

			err := agent.Container.CancelAllStreams()
			if err != nil {
				log.Error().Err(err).Msg("error closing stream")
			}

			for {
				agent.Messenger.Reconnect()

				// did we reconnect successfully? new internal client should be set now
				if agent.Messenger.Connected() {
					safe.Go(func() {
						log.Debug().Msg("Reconnect: was able to reconnect, running setup again")
						err := agent.OnConnect()
						if err != nil {
							log.Fatal().Stack().Err(err).Msg("failed to run on connect handler")
						}

						agent.ListenForDisconnect()

						log.Debug().Msg("Reconnect: Successfully reconnected main session")
					})
					break reconnect
				}

				log.Debug().Msg("Reconnect: it appears the socket disconnected right after reconnecting, retrying...")
				agent.Messenger.Reconnect()

				time.Sleep(time.Second * 1)
			}

		}
		log.Debug().Msg("Reconnect: Reconnect signal goroutine has exited")
	})

	log.Debug().Msg("Reconnect: Setup Reconnect loop")
}
