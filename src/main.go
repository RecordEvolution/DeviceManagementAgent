package main

import (
	"os"
	"os/signal"
	"reagent/api"
	"reagent/apps"
	"reagent/config"
	"reagent/container"
	"reagent/logging"
	"reagent/messenger"
	"reagent/persistence"
	"reagent/system"
	"reagent/terminal"

	"github.com/rs/zerolog/log"
)

func main() {
	cliArgs := config.GetCliArguments()
	logging.SetupLogger(cliArgs)

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load reswarm config file")
	}

	generalConfig := config.Config{
		ReswarmConfig:        reswarmConfig,
		CommandLineArguments: cliArgs,
	}

	stateStorer, _ := persistence.NewSQLiteDb(&generalConfig)
	err = stateStorer.Init()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initalize SQLite database")
	}

	messenger, err := messenger.NewWamp(generalConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to setup wamp connection")
	}

	system.UpdateRemoteDeviceStatus(messenger, system.CONNECTED)

	container, _ := container.NewDocker(generalConfig)

	stateUpdater := apps.StateUpdater{
		StateStorer: stateStorer,
		Messenger:   messenger,
		Container:   container,
	}

	stateObserver := apps.StateObserver{
		StateUpdater: stateUpdater,
	}

	logManager := logging.LogManager{
		Messenger: messenger,
		Container: container,
	}

	stateMachine := apps.StateMachine{
		StateObserver: stateObserver,
		StateUpdater:  stateUpdater,
		Container:     container,
		LogManager:    &logManager,
	}

	stateSyncer := apps.StateSyncer{
		StateMachine: stateMachine,
		StateUpdater: stateUpdater,
	}

	err = stateSyncer.Sync()

	if err != nil {
		log.Fatal().Err(err).Msg("failed to run sync")
	}

	terminalManager := terminal.New(messenger, container)

	external := api.External{
		StateMachine:    &stateMachine,
		Config:          &generalConfig,
		LogManager:      &logManager,
		TerminalManager: &terminalManager,
		StateUpdater:    &stateUpdater,
		Messenger:       messenger,
		StateStorer:     stateStorer,
	}

	external.RegisterAll()

	appStates, err := stateStorer.GetAppStates()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get local app states")
	}

	logManager.Init()
	logManager.ReviveDeadLogs(appStates)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
