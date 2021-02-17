package main

import (
	"context"
	"os"
	"os/signal"
	"reagent/api"
	"reagent/apps"
	"reagent/config"
	"reagent/container"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
	"reagent/store"
	"reagent/system"
	"reagent/terminal"

	"github.com/rs/zerolog/log"
)

func InitialSetup(
	external *api.External,
	messenger messenger.Messenger,
	database persistence.Database,
	appManager *apps.AppManager,
	logManager *logging.LogManager,
	stateObserver *apps.StateObserver,
) error {
	err := stateObserver.CorrectLocalAndUpdateRemoteAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to CorrectLocalAndUpdateRemoteAppStates")
	}

	err = appManager.Sync()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to sync")
	}

	apps, err := database.GetAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to get local app states")
	}

	err = stateObserver.ObserveAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init app state observers")
	}

	logManager.SetupWampSubscriptions()
	logManager.ReviveDeadLogs(apps)

	external.RegisterAll()

	err = system.UpdateRemoteDeviceStatus(messenger, system.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	return err
}

func main() {
	cliArgs := config.GetCliArguments()
	logging.SetupLogger(cliArgs)

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to load reswarm config file")
	}

	generalConfig := config.Config{
		ReswarmConfig:        reswarmConfig,
		CommandLineArguments: cliArgs,
	}

	database, _ := persistence.NewSQLiteDb(&generalConfig)
	err = database.Init()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to initalize SQLite database")
	}

	messenger, err := messenger.NewWamp(&generalConfig)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup wamp connection")
	}

	err = messenger.SetupTestament()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to setup testament")
	}

	container, _ := container.NewDocker(&generalConfig)

	appStore := store.NewAppStore(database, messenger)

	stateObserver := apps.NewObserver(container, &appStore)

	logManager := logging.LogManager{
		Messenger: messenger,
		Container: container,
	}

	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver)
	appManager := apps.NewAppManager(&stateMachine, &appStore, &stateObserver)

	terminalManager, err := terminal.NewTerminalManager(messenger, container)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed init terminal manager")
	}

	external := api.External{
		Config:          &generalConfig,
		LogManager:      &logManager,
		TerminalManager: &terminalManager,
		AppManager:      &appManager,
		Messenger:       messenger,
		Database:        database,
	}

	err = InitialSetup(&external, messenger, database, &appManager, &logManager, &stateObserver)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init")
	}

	go func() {
		doneSignal := messenger.Done()
		dead := false

	reconnectLoop:
		for {
			select {
			case <-doneSignal:
				// done signal received, we most likely disconnected

				messenger.Close()
				// attempt to reconnect
				err := messenger.ResetSession(context.Background())
				if err != nil {
					log.Fatal().Stack().Err(err).Msg("failed to reconnect, retrying...")
				}

				dead = true

			default:
				if !dead {
					break
				}
				// did we reconnect successfully? new internal client should be set now
				if messenger.Connected() {
					doneSignal = messenger.Done()

					// rerun setup
					err = InitialSetup(&external, messenger, database, &appManager, &logManager, &stateObserver)
					if err != nil {
						log.Fatal().Stack().Err(err).Msg("failed to init")
						break
					}

					dead = false
					break reconnectLoop
				}
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
