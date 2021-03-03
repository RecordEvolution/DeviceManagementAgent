package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reagent/benchmark"
	"reagent/config"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/system"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/rs/zerolog/log"
)

func main() {
	defer func() {
		err := recover()

		if err != nil {
			log.Fatal().Msgf("Panic: %+v \n Stack Trace: %s", err, debug.Stack())
		}
	}()

	benchmark.AgentInit = time.Now()
	benchmark.PreConnectInit = time.Now()
	benchmark.OnConnectInit = time.Now()
	benchmark.SocketConnectionInitFromLaunch = time.Now()
	benchmark.GreenInit = time.Now()

	cliArgs, err := config.GetCliArguments()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to parse cli arguments")
	}

	// print version string to stdout
	if cliArgs.Version {
		fmt.Println(system.GetVersion())
		os.Exit(0)
	}

	logging.SetupLogger(cliArgs)

	log.Info().Msgf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s)", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s) \n\n", runtime.GOOS, runtime.GOARCH)

	err = filesystem.InitDirectories(cliArgs)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init reagent directories")
	}

	if cliArgs.EnsureNameserver {
		err = filesystem.EnsureResolvConf()
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("failed to ensure resolv.conf")
		}
	}

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to load reswarm config file")
	}

	generalConfig := config.New(cliArgs, reswarmConfig)

	agent := NewAgent(&generalConfig)

	agent.ListenForDisconnect()

	fmt.Println("Waiting for Docker Daemon to be available...")
	log.Info().Msg("Waiting for Docker Daemon to be available...")
	err = agent.Container.WaitForDaemon(context.Background())
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("error occured while waiting for daemon")
	}
	log.Info().Msg("Got reply from Docker Daemon, continuing")
	fmt.Println("Got reply from Docker Daemon, continuing")

	err = agent.SetupConnectionStatusHeartbeat()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed setup connection status heartbeat")
	}

	log.Info().Msg("Running onConnect handler")
	fmt.Println("Running onConnect handler")
	err = agent.OnConnect()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init")
	}
	log.Info().Msg("OnConnect handler finished")
	fmt.Println("OnConnect handler finished")

	benchmark.TimeTillOnConnectAfterConnection = time.Since(benchmark.OnConnectInitAfterConnection)
	benchmark.TimeTillOnConnect = time.Since(benchmark.OnConnectInit)
	benchmark.TimeTillAgentInit = time.Since(benchmark.AgentInit)
	log.Info().Msg("Finished Reagent initialization sequence")

	fmt.Println()
	fmt.Println("Finished Reagent Initialisation")
	fmt.Println()

	benchmark.LogResults()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
