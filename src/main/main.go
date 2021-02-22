package main

import (
	"os"
	"os/signal"
	"reagent/config"
	"reagent/filesystem"
	"reagent/logging"

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

	generalConfig := config.New(cliArgs, reswarmConfig)

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
