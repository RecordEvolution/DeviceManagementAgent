package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reagent/config"
	"reagent/filesystem"
	"reagent/logging"
	"runtime"

	"github.com/rs/zerolog/log"
)

func main() {
	defer func() {
		if err := recover(); err != nil {
			log.Fatal().Str("panic", fmt.Sprint(err)).Msgf("recovered from a panic, will exit...")
			os.Exit(1)
		}
	}()

	cliArgs, err := config.GetCliArguments()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to parse cli arguments")
	}

	logging.SetupLogger(cliArgs)

	log.Info().Msgf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s)", runtime.GOOS, runtime.GOARCH)

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

	log.Info().Msg("Waiting for Docker Daemon to be available...")
	err = agent.Container.WaitForDaemon(context.Background())
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("error occured while waiting for daemon")
	}
	log.Info().Msg("Got reply from Docker Daemon, continuing")

	err = agent.OnConnect()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init")
	}

	log.Info().Msg("Finished Reagent initialization sequence")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
