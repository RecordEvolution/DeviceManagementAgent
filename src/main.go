package main

import (
	"context"
	"os"
	"os/signal"
	"reagent/config"
	"reagent/filesystem"
	"reagent/logging"
	"runtime"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

func main() {
	defer func() {
		err := recover()

		if err != nil {
			log.Fatal().Msgf("Panic: %+v \n Stack Trace: %s", err, debug.Stack())
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

	err = filesystem.EnsureResolvConf()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to ensure resolve.conf")
	}

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to load reswarm config file")
	}

	generalConfig := config.New(cliArgs, reswarmConfig)

	agent := NewAgent(&generalConfig)

	agent.ListenForDisconnect()

	log.Info().Msg("Waiting for Docker Daemon to be available...")
	err = agent.Container.WaitForDaemon(context.Background())
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("error occured while waiting for daemon")
	}
	log.Info().Msg("Got reply from Docker Daemon, continuing")

	err = agent.SetupConnectionStatusHeartbeat()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed setup connection status heartbeat")
	}

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
