package main

import (
	"context"
	"os"
	"os/signal"
	"reagent/api"
	"reagent/apps"
	"reagent/config"
	"reagent/container"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"

	"github.com/rs/zerolog/log"
)

func main() {
	cliArgs, err := config.GetCliArguments()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to parse cli arguments")
	}

	logging.SetupLogger(cliArgs)
	err = filesystem.InitDirectories(cliArgs)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init reagent directories")
	}

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to load reswarm config file")
	}

	generalConfig := config.Config{
		ReswarmConfig:        reswarmConfig,
		CommandLineArguments: cliArgs,
	}

	agent := NewAgent(&generalConfig)
	err = agent.Init()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init")
	}

	agent.ListenForDisconnect()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}

type Agent struct {
	Messenger       messenger.Messenger
	Database        persistence.Database
	Config          *config.Config
	External        *api.External
	LogManager      *logging.LogManager
	TerminalManager *terminal.TerminalManager
	AppManager      *apps.AppManager
	StateObserver   *apps.StateObserver
	StateMachine    *apps.StateMachine
}

func (agent *Agent) Init() error {
	err := system.UpdateRemoteDeviceStatus(agent.Messenger, system.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
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

	return err
}

func NewAgent(generalConfig *config.Config) (agent *Agent) {
	database, _ := persistence.NewSQLiteDb(generalConfig)
	err := database.Init()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to initalize SQLite database")
	}

	messenger, err := messenger.NewWamp(generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup wamp connection")
	}

	err = messenger.SetupTestament()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup testament")
	}

	container, err := container.NewDocker(generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup docker")
	}

	appStore := store.NewAppStore(database, messenger)

	stateObserver := apps.NewObserver(container, &appStore)

	logManager := logging.LogManager{
		Messenger: messenger,
		AppStore:  appStore,
		Database:  database,
		Container: container,
	}

	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver)
	appManager := apps.NewAppManager(&stateMachine, &appStore, &stateObserver)

	terminalManager, err := terminal.NewTerminalManager(messenger, container)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed init terminal manager")
	}

	external := api.External{
		Container:       container,
		Messenger:       messenger,
		Database:        database,
		AppManager:      &appManager,
		TerminalManager: &terminalManager,
		LogManager:      &logManager,
		Config:          generalConfig,
	}

	return &Agent{
		Config:          generalConfig,
		External:        &external,
		LogManager:      &logManager,
		TerminalManager: &terminalManager,
		AppManager:      &appManager,
		StateObserver:   &stateObserver,
		StateMachine:    &stateMachine,
		Messenger:       messenger,
		Database:        database,
	}
}

func (agent *Agent) ListenForDisconnect() {
	go func() {
		doneSignal := agent.Messenger.Done()
		reconnectSignal := make(chan struct{})

		log.Debug().Msg("Reconnect: initialized reconnect goroutine")

		go func() {
			select {
			case <-doneSignal:
				log.Debug().Msg("Reconnect: received a done signal for WAMP connection")

				err := agent.Messenger.Close()
				if err != nil {
					log.Fatal().Stack().Err(err).Msg("failed to close agent.Messenger...")
				}
				log.Debug().Msg("Reconnect: cleaned up the old session")

				log.Debug().Msg("Reconnect: creating new session...")
				err = agent.Messenger.ResetSession(context.Background())
				if err != nil {
					log.Fatal().Stack().Err(err).Msg("failed to reconnect, retrying...")
				}

				reconnectSignal <- struct{}{}

				break
			}
		}()

		go func() {
			select {
			case <-reconnectSignal:
				log.Debug().Msg("Reconnect: we are dead, let's try to reconnect..")
				// did we reconnect successfully? new internal client should be set now
				if agent.Messenger.Connected() {
					doneSignal = agent.Messenger.Done()
					log.Debug().Msg("Reconnect: was able to reconnect, running setup again")

					// rerun setup
					err := agent.Init()
					if err != nil {
						log.Fatal().Stack().Err(err).Msg("failed to init")
						break
					}

					log.Debug().Msg("Reconnect: setup complete, fully reconnected!")

					agent.ListenForDisconnect()
					return
				}
			}

		}()
	}()
}
