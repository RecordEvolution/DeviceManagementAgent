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
	"reagent/persistence"
	"reagent/safe"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"
	"time"

	"github.com/rs/zerolog/log"
)

type Agent struct {
	Container       container.Container
	Messenger       messenger.Messenger
	LogMessenger    messenger.Messenger
	Database        persistence.Database
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

	err := agent.LogManager.SetupEndpoints()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup endpoints")
	}

	err = agent.External.RegisterAll()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to register all external endpoints")
	}

	// first call this in case we don't have any app state yet, then we can start containers accordingly
	err = agent.AppManager.UpdateLocalRequestedAppStatesWithRemote()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to sync")
	}

	err = agent.StateObserver.CorrectLocalAndUpdateRemoteAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to CorrectLocalAndUpdateRemoteAppStates")
	}

	err = agent.AppManager.EvaluateRequestedStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to EvaluateRequestedStates")
	}

	err = agent.StateObserver.ObserveAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init app state observers")
	}

	err = agent.LogManager.ReviveDeadLogs()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to revive dead logs")
	}

	err = agent.Messenger.UpdateRemoteDeviceStatus(messenger.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	benchmark.TimeTillGreen = time.Since(benchmark.GreenInit)

	return err
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

	err = stateObserver.CorrectLocalAndUpdateRemoteAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to correct local states")
	}

	// setup the containers on start
	err = appManager.EnsureLocalRequestedStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to ensure local app states")
	}

	// try to establish the main session
	mainSocketConfig := messenger.SocketConfig{
		PingPongTimeout: 5 * time.Second,
		ResponseTimeout: 5 * time.Second,
		SetupTestament:  true,
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
		Filesystem:      &filesystem,
		System:          &systemAPI,
		AppManager:      &appManager,
		TerminalManager: &terminalManager,
		LogManager:      &logManager,
		Config:          generalConfig,
	}

	return &Agent{
		Config:          generalConfig,
		System:          &systemAPI,
		External:        &external,
		LogManager:      &logManager,
		TerminalManager: &terminalManager,
		AppManager:      &appManager,
		StateObserver:   &stateObserver,
		StateMachine:    &stateMachine,
		Filesystem:      &filesystem,
		Container:       container,
		Messenger:       mainSession,
		LogMessenger:    mainSession,
		Database:        database,
	}
}

func (agent *Agent) SetupConnectionStatusHeartbeat() error {
	safe.Go(func() {
		for {
			if !agent.Messenger.Connected() {
				continue
			}

			config := agent.Config
			payload := common.Dict{
				"swarm_key":       config.ReswarmConfig.SwarmKey,
				"device_key":      config.ReswarmConfig.DeviceKey,
				"status":          messenger.CONNECTED,
				"wamp_session_id": agent.Messenger.GetSessionID(),
			}

			ctx, cancelFunc := context.WithTimeout(context.Background(), 500*time.Millisecond)
			options := common.Dict{"timeout": 500}
			agent.Messenger.Call(ctx, topics.UpdateDeviceStatus, []interface{}{payload}, nil, options, nil)

			cancelFunc()

			time.Sleep(time.Second * 5)
		}
	})

	return nil
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
