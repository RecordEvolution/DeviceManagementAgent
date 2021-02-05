package main

import (
	"fmt"
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
)

func main() {
	reswarmConfig, err := config.LoadReswarmConfig("./demo_demo_swarm_TestDevice.reswarm")
	if err != nil {
		panic(err)
	}
	cliArgs := config.CommandLineArguments{
		AppBuildsDirectory:       "/Users/ruben/Desktop",
		CompressedBuildExtension: ".tgz",
	}

	generalConfig := config.Config{
		ReswarmConfig:        reswarmConfig,
		CommandLineArguments: &cliArgs,
	}

	stateStorer, _ := persistence.NewSQLiteDb(&generalConfig)
	err = stateStorer.Init()
	if err != nil {
		panic(err)
	}

	messenger, _ := messenger.NewWamp(generalConfig)
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
		Messenger:         messenger,
		BuildContainerMap: map[string]string{},
	}

	stateMachine := apps.StateMachine{
		StateObserver: stateObserver,
		StateUpdater:  stateUpdater,
		Container:     container,
		LogManager:    logManager,
	}

	stateSyncer := apps.StateSyncer{
		StateMachine: stateMachine,
		StateUpdater: stateUpdater,
	}

	err = stateSyncer.Sync()

	if err != nil {
		fmt.Println(err)
	}

	external := api.External{
		Messenger:    messenger,
		StateMachine: stateMachine,
		Config:       &generalConfig,
		LogManager:   logManager,
		StateUpdater: stateUpdater,
		StateStorer:  stateStorer,
	}

	external.RegisterAll()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
