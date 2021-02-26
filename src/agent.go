package main

import (
	"reagent/api"
	"reagent/apps"
	"reagent/config"
	"reagent/container"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
	"reagent/safe"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"

	"github.com/rs/zerolog/log"
)

type Agent struct {
	Container       container.Container
	Messenger       messenger.Messenger
	Database        persistence.Database
	System          *system.System
	Config          *config.Config
	External        *api.External
	LogManager      *logging.LogManager
	TerminalManager *terminal.TerminalManager
	AppManager      *apps.AppManager
	StateObserver   *apps.StateObserver
	StateMachine    *apps.StateMachine
}

func (agent *Agent) OnConnect() error {
	updateResult, err := agent.System.UpdateIfRequired(nil)
	if err != nil {
		log.Error().Stack().Err(err).Msgf("Failed to update.. continuing...")
	}

	if updateResult.DidUpdate {
		log.Debug().Msgf("Successfully downloaded new Reagent (v%s)", updateResult.CurrentVersion)
	}

	err = agent.Messenger.SetupTestament()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup testament")
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

	err = agent.LogManager.SetupEndpoints()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup endpoints")
	}

	err = agent.LogManager.ReviveDeadLogs()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to revive dead logs")
	}

	err = agent.External.RegisterAll()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to register all external endpoints")
	}

	err = system.UpdateRemoteDeviceStatus(agent.Messenger, system.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	agent.ListenForDisconnect()

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

	appStore := store.NewAppStore(database, dummyMessenger)
	stateObserver := apps.NewObserver(container, &appStore)
	logManager := logging.NewLogManager(container, dummyMessenger, database, appStore)
	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver)
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

	// try to establish a wamp connection
	wamp, err := messenger.NewWamp(generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup wamp connection")
	}

	// established a connection, replace the dummy messenger
	appStore.SetMessenger(wamp)
	logManager.SetMessenger(wamp)
	terminalManager.SetMessenger(wamp)
	terminalManager.InitUnregisterWatcher()

	external := api.External{
		Container:       container,
		Messenger:       wamp,
		Database:        database,
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
		Container:       container,
		Messenger:       wamp,
		Database:        database,
	}
}

func (agent *Agent) ListenForDisconnect() {
	safe.Go(func() {
		doneSignal := agent.Messenger.Done()
		reconnectSignal := make(chan struct{})

		safe.Go(func() {
			select {
			case <-doneSignal:
				log.Debug().Msg("Reconnect: attempting to create a new session...")
				agent.Messenger.Reconnect() // will block until a session is established

				reconnectSignal <- struct{}{}
				break
			}
		})

		safe.Go(func() {
			select {
			case <-reconnectSignal:
				// did we reconnect successfully? new internal client should be set now
				if agent.Messenger.Connected() {
					doneSignal = agent.Messenger.Done()
					log.Debug().Msg("Reconnect: was able to reconnect, running setup again")

					// rerun setup
					err := agent.OnConnect()
					if err != nil {
						log.Fatal().Stack().Err(err).Msg("failed to run on connect handler")
						break
					}

					log.Debug().Msg("Reconnect: setup complete, fully reconnected!")

					agent.ListenForDisconnect()
					return
				}
			}
		})

		log.Debug().Msg("Reconnect: Set up WAMP reconnect listeners")
	})
}
