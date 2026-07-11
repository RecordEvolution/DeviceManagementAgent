package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"reagent/benchmark"

	// "reagent/common"
	"reagent/config"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/release"
	"reagent/safe"

	"runtime"
	"runtime/debug"
	"time"

	_ "net/http/pprof"

	"github.com/rs/zerolog/log"
)

func main() {
	defer func() {
		err := recover()

		if err != nil {
			log.Fatal().Msgf("Panic: %+v \n Stack Trace: %s", err, debug.Stack())
		}
	}()

	// `reagent service install|uninstall|start|stop|status` manages the
	// Windows service. It must be dispatched before GetCliArguments: the
	// global flag package stops parsing at the first positional argument.
	if len(os.Args) > 1 && os.Args[1] == "service" {
		os.Exit(runServiceCommand(os.Args[2:]))
	}

	cliArgs, err := config.GetCliArguments()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Failed to get CLI args")
	}

	// if release.BuildArch == "" && cliArgs.Environment != string(common.LOCAL) {
	// 	fmt.Println("The 'reagent/release.BuildArch' build flag was not included during the build of this version.")
	// 	os.Exit(1)
	// }

	if cliArgs.ConfigFileLocation == "" {
		if cliArgs.Version {
			fmt.Println(release.GetVersion())
			os.Exit(0)
		}

		if cliArgs.Arch {
			fmt.Println(release.BuildArch)
			os.Exit(0)
		}

		fmt.Println("'-config' argument is required. -help for usage")
		os.Exit(0)
	}

	// Refuse a second agent on the machine (a service + console double-run
	// makes the two agents delete each other's containers) and, on Windows,
	// reap child processes on any exit.
	err = acquireSingleInstanceLock()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to acquire single-instance lock")
	}
	setupProcessJobObject()

	if runningAsService() {
		runService(cliArgs)
		return
	}

	agent, err := runAgent(cliArgs)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed to start agent")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	<-sigChan

	agent.Shutdown(time.Second * 10)
}

// runAgent brings up the full agent (directories, config, docker, WAMP,
// OnConnect) and returns once the agent is operational. Shared by console mode
// and the Windows service.
func runAgent(cliArgs *config.CommandLineArguments) (*Agent, error) {
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

	}

	logging.SetupLogger(cliArgs)

	benchmark.PreConnectInit = time.Now()
	benchmark.OnConnectInit = time.Now()
	benchmark.SocketConnectionInitFromLaunch = time.Now()
	benchmark.GreenInit = time.Now()

	startupLogChannel := make(chan string)
	safe.Go(func() {
		for logMsg := range startupLogChannel {
			if !cliArgs.PrettyLogging {
				fmt.Println(logMsg)
			} else {
				log.Info().Msg(logMsg)
			}
		}
	})

	startupLogChannel <- fmt.Sprintf("Starting... Reagent initialization sequence (OOS: %s, ARCH: %s)", runtime.GOOS, runtime.GOARCH)

	err := filesystem.InitDirectories(cliArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to init reagent directories: %w", err)
	}

	reswarmConfig, err := config.LoadReswarmConfig(cliArgs.ConfigFileLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to load .flock config file: %w", err)
	}

	generalConfig := config.New(cliArgs, reswarmConfig)
	generalConfig.StartupLogChannel = startupLogChannel

	agent := NewAgent(&generalConfig)

	safe.Go((func() {
		startupLogChannel <- "Checking for agent update in background..."

		_, err := agent.System.UpdateSystem(nil, agent.Config.CommandLineArguments.ShouldUpdateAgent)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to update system")
		}
	}))

	startupLogChannel <- "Running onConnect handler"

	err = agent.OnConnect(false)
	if err != nil {
		return nil, fmt.Errorf("failed to run the onConnect handler: %w", err)
	}

	benchmark.TimeTillOnConnectAfterConnection = time.Since(benchmark.OnConnectInitAfterConnection)
	benchmark.TimeTillOnConnect = time.Since(benchmark.OnConnectInit)

	benchmark.LogBenchmarkResultsWhenFinished(&generalConfig)

	return agent, nil
}
