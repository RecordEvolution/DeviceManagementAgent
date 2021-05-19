package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"reagent/benchmark"
	"reagent/config"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/safe"

	"reagent/system"
	"runtime"
	"runtime/debug"
	"time"

	profile "github.com/bygui86/multi-profile/v2"
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
		if errors.Is(err, errdefs.ErrConfigNotProvided) {
			fmt.Println("'-config' argument is required. -help for usage")
			os.Exit(0)
		} else {
			log.Fatal().Stack().Err(err).Msg("Failed to GetCliArguments")
		}
	}

	if cliArgs.Profiling {
		port := cliArgs.ProfilingPort
		if cliArgs.ProfilingPort == 0 {
			port = 80
		}

		safe.Go(func() {
			err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("Failed to init web server")
			}
		})

		defer profile.CPUProfile(&profile.Config{}).Start().Stop()
		defer profile.BlockProfile(&profile.Config{}).Start().Stop()
		defer profile.GoroutineProfile(&profile.Config{}).Start().Stop()
		defer profile.MutexProfile(&profile.Config{}).Start().Stop()
		defer profile.MemProfile(&profile.Config{}).Start().Stop()
	}

	if cliArgs.Version {
		fmt.Println(system.GetVersion())
		os.Exit(0)
	}

	logging.SetupLogger(cliArgs)

	benchmark.PreConnectInit = time.Now()
	benchmark.OnConnectInit = time.Now()
	benchmark.SocketConnectionInitFromLaunch = time.Now()
	benchmark.GreenInit = time.Now()

	log.Info().Msgf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s)", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s) \n\n", runtime.GOOS, runtime.GOARCH)

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
	agent.ListenForDisconnect()

	fmt.Println("Waiting for Docker Daemon to be available...")
	log.Info().Msg("Waiting for Docker Daemon to be available...")

	err = agent.Container.WaitForDaemon()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("error occured while waiting for daemon")
	}

	log.Info().Msg("Got reply from Docker Daemon, continuing")
	fmt.Println("Got reply from Docker Daemon, continuing")

	log.Info().Msg("Running onConnect handler")
	fmt.Println("Running onConnect handler")
	err = agent.OnConnect()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to init")
	}
	log.Info().Msg("OnConnect handler finished")
	fmt.Println("OnConnect handler finished")

	agent.InitConnectionStatusHeartbeat()

	benchmark.TimeTillOnConnectAfterConnection = time.Since(benchmark.OnConnectInitAfterConnection)
	benchmark.TimeTillOnConnect = time.Since(benchmark.OnConnectInit)

	benchmark.LogBenchmarkResultsWhenFinished()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
		return
	}
}
