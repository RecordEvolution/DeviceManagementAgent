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

	err = system.UpdateRemoteDeviceStatus(messenger, system.CONNECTED)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to update remote device status")
	}

	container, _ := container.NewDocker(&generalConfig)

	appStore := apps.NewAppStore(database, messenger)

	stateObserver := apps.NewObserver(container, &appStore)

	logManager := logging.LogManager{
		Messenger: messenger,
		Container: container,
	}

	stateMachine := apps.NewStateMachine(container, &logManager, &stateObserver)
	appManager := apps.NewAppManager(&stateMachine, &appStore)

	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to run sync")
	}

	terminalManager, err := terminal.InitManager(messenger, container)
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

	external.RegisterAll()

	apps, err := database.GetAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to get local app states")
	}

	err = appManager.Sync()

	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to sync")
	}

	err = stateObserver.ObserveAppStates()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init app state observers")
	}

	logManager.Init()
	logManager.ReviveDeadLogs(apps)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
