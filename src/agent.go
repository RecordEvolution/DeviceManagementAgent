package main

import (
	"fmt"
	"reagent/api"
	"reagent/apps"
	"reagent/benchmark"
	"reagent/config"
	"reagent/container"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/messenger"
	"reagent/network"
	"reagent/persistence"
	"reagent/safe"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"
	"runtime"
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
	Filesystem      *filesystem.Filesystem
	AppManager      *apps.AppManager
	StateObserver   *apps.StateObserver
	StateMachine    *apps.StateMachine
}

func (agent *Agent) OnConnect() error {
	if agent.Config.CommandLineArguments.ShouldUpdate {
		safe.Go(func() {
			updateResult, err := agent.System.UpdateIfRequired(nil)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("Failed to update.. continuing...")
			}

			if updateResult.DidUpdate {
				log.Debug().Msgf("Successfully downloaded new Reagent (v%s)", updateResult.CurrentVersion)
			}
		})
	}

	// first call this in case we don't have any app state yet, then we can start containers accordingly
	err := agent.AppManager.UpdateLocalRequestedAppStatesWithRemote()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to sync")
	}

	err = agent.StateObserver.CorrectLocalAndUpdateRemoteAppStates()
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

	safe.Go(func() {
		err = agent.LogManager.ReviveDeadLogs()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to revive dead logs")
		}

		err := agent.LogManager.SetupEndpoints()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup endpoints")
		}
	})

	safe.Go(func() {
		err = agent.External.RegisterAll()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to register all external endpoints")
		}

		err = agent.Messenger.UpdateRemoteDeviceStatus(messenger.CONNECTED)
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
		}

		benchmark.TimeTillGreen = time.Since(benchmark.GreenInit)
	})

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

func NewAgent(generalConfig *config.Config) (agent *Agent) {

	systemAPI := system.New(generalConfig)

	database, _ := persistence.NewSQLiteDb(generalConfig)
	err := database.Init()
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
	appStore := store.NewAppStore(database, dummyMessenger)
	stateObserver := apps.NewObserver(container, &appStore)
	logManager := logging.NewLogManager(container, dummyMessenger, database, appStore)
	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver, &filesystem)
	appManager := apps.NewAppManager(&stateMachine, &appStore, &stateObserver)
	terminalManager := terminal.NewTerminalManager(dummyMessenger, container)

	var networkInstance network.Network
	if runtime.GOOS == "linux" {
		networkInstance, err = network.NewNMWNetwork()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to setup network")
		}
	} else {
		// TODO: write implementations for other environments. (issue: https://github.com/RecordEvolution/DeviceManagementAgent/issues/41)
		networkInstance = network.NewDummyNetwork()
	}

	err = stateObserver.CorrectLocalAndUpdateRemoteAppStates()
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
	cliArgs := generalConfig.CommandLineArguments
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

	mainSession, err := messenger.NewWamp(generalConfig, &mainSocketConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup wamp connection")
	}

	benchmark.TimeTillSocketConnection = time.Since(benchmark.SocketConnectionInit)
	benchmark.TimeTillSocketConnectionFromLaunch = time.Since(benchmark.SocketConnectionInitFromLaunch)

	fmt.Println("Connected!")

	// try to establish the log session
	// logSession, _ := messenger.NewWamp(generalConfig, &messenger.SocketConfig{})

	// established a connection, replace the dummy messenger
	appStore.SetMessenger(mainSession)
	terminalManager.SetMessenger(mainSession)
	terminalManager.InitUnregisterWatcher()

	logManager.SetMessenger(mainSession)

	external := api.External{
		Container:       container,
		Messenger:       mainSession,
		LogMessenger:    mainSession,
		Database:        database,
		Network:         networkInstance,
		Filesystem:      &filesystem,
		System:          &systemAPI,
		AppManager:      appManager,
		TerminalManager: &terminalManager,
		LogManager:      &logManager,
		Config:          generalConfig,
	}

	return &Agent{
		Config:          generalConfig,
		System:          &systemAPI,
		External:        &external,
		LogManager:      &logManager,
		Network:         networkInstance,
		TerminalManager: &terminalManager,
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
