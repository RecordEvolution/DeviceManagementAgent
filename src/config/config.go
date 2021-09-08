package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
)

// ReswarmConfig types for the .reswarm file
type ReswarmConfig struct {
	Name           string      `json:"name"`
	Secret         string      `json:"secret"`
	Status         string      `json:"status"`
	Password       string      `json:"password"`
	Wlanssid       string      `json:"wlanssid"`
	SwarmKey       int         `json:"swarm_key"`
	DeviceKey      int         `json:"device_key"`
	SwarmName      string      `json:"swarm_name"`
	Description    interface{} `json:"description"` // can be null --> interface{}
	Architecture   string      `json:"architecture"`
	SerialNumber   string      `json:"serial_number"`
	Authentication struct {
		Key         string `json:"key"`
		Certificate string `json:"certificate"`
	} `json:"authentication"`
	SwarmOwnerName       string `json:"swarm_owner_name"`
	ConfigPassphrase     string `json:"config_passphrase"`
	DeviceEndpointURL    string `json:"device_endpoint_url"`
	DockerRegistryURL    string `json:"docker_registry_url"`
	InsecureRegistries   string `json:"insecure-registries"`
	DockerMainRepository string `json:"docker_main_repository"`
	ReswarmBaseURL       string
}

type CommandLineArguments struct {
	AppsDirectory              string
	AppsBuildDir               string
	AppsSharedDir              string
	AgentDir                   string
	AgentDownloadDir           string
	CompressedBuildExtension   string
	RemoteUpdateURL            string
	Debug                      bool
	DebugMessaging             bool
	Version                    bool
	Arch                       bool
	Offline                    bool
	Profiling                  bool
	ProfilingPort              uint
	ShouldUpdate               bool
	ForceUpdate                bool
	PrettyLogging              bool
	LogFileLocation            string
	ConfigFileLocation         string
	DatabaseFileName           string
	PingPongTimeout            uint
	ResponseTimeout            uint
	ConnectionEstablishTimeout uint
}

type Config struct {
	ReswarmConfig        *ReswarmConfig
	CommandLineArguments *CommandLineArguments
}

func New(cliArgs *CommandLineArguments, reswarmConfig *ReswarmConfig) Config {
	return Config{
		ReswarmConfig:        reswarmConfig,
		CommandLineArguments: cliArgs,
	}
}

func GetCliArguments() (*CommandLineArguments, error) {
	defaultAgentDir := "/opt/reagent"
	defaultLogFilePath := "/var/log/reagent.log"

	// fallback for when reagent is ran on mac/windows
	if runtime.GOOS != "linux" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		defaultLogFilePath = fmt.Sprintf("%s/%s", homeDir, "reagent/reagent.log")
		defaultAgentDir = fmt.Sprintf("%s/%s", homeDir, "reagent")
	}

	// By default apps are stored inside the default agent directory, as well, to
	// support a variety of distributions/systems with a rootfs only. However,
	// for the agent running on actual embedded linux we tend to use a separate
	// dedicated (update-)persistent partition mounted at "/apps"
	defaultAppsDir := defaultAgentDir + "/apps"

	logFile := flag.String("logFile", defaultLogFilePath, "log file used by the reagent")
	debug := flag.Bool("debug", true, "sets the log level to debug")
	forceUpdate := flag.Bool("forceUpdate", false, "forces the agent to download the latest version")
	shouldUpdate := flag.Bool("update", true, "determines if the agent should update on start")
	offline := flag.Bool("offline", false, "starts the agent without establishing a socket connection. meant for debugging")
	arch := flag.Bool("arch", false, "displays the version of ARM that was used during the build of the binary")
	version := flag.Bool("version", false, "displays the current version of the agent")
	profiling := flag.Bool("profiling", false, "spins up a pprof webserver on the defined port")
	profilingPort := flag.Uint("profilingPort", 80, "port of the profiling service")
	prettyLogging := flag.Bool("prettyLogging", false, "enables the pretty console writing, intended for debugging")
	remoteUpdateURL := flag.String("remoteUpdateURL", "https://storage.googleapis.com/re-agent", "used to download new versions of the agent and check for updates")
	agentDir := flag.String("agentDir", defaultAgentDir, "default location of the agent binary")
	appsDir := flag.String("appsDir", defaultAppsDir, "default path for apps and app-data")
	databaseFileName := flag.String("dbFileName", "reagent.db", "defines the name used to persist the database file")
	debugMessaging := flag.Bool("debugMessaging", false, "enables debug logs for messenging layer")
	compressedBuildExtension := flag.String("compressedBuildExtension", "tgz", "sets the extension in which the compressed build files will be provided")
	pingPongTimeout := flag.Uint("ppTimeout", 0, "Sets the ping pong timeout of the client in milliseconds (0 means no timeout)")
	responseTimeout := flag.Uint("respTimeout", 5000, "Sets the response timeout of the client in milliseconds")
	socketConnectionEstablishTimeout := flag.Uint("connTimeout", 1250, "Sets the connection timeout for the socket connection in milliseconds. (0 means no timeout)")
	cfgFile := flag.String("config", "", "reswarm configuration file")
	flag.Parse()

	cliArgs := CommandLineArguments{
		AppsDirectory:              *appsDir,
		AppsBuildDir:               (*appsDir) + "/build",
		AppsSharedDir:              (*appsDir) + "/shared",
		AgentDownloadDir:           (*agentDir) + "/downloads",
		AgentDir:                   *agentDir,
		RemoteUpdateURL:            *remoteUpdateURL,
		CompressedBuildExtension:   *compressedBuildExtension,
		Debug:                      *debug,
		Version:                    *version,
		Offline:                    *offline,
		PrettyLogging:              *prettyLogging,
		DebugMessaging:             *debugMessaging,
		LogFileLocation:            *logFile,
		ConfigFileLocation:         *cfgFile,
		Profiling:                  *profiling,
		ProfilingPort:              *profilingPort,
		ShouldUpdate:               *shouldUpdate,
		ForceUpdate:                *forceUpdate,
		DatabaseFileName:           *databaseFileName,
		PingPongTimeout:            *pingPongTimeout,
		ResponseTimeout:            *responseTimeout,
		ConnectionEstablishTimeout: *socketConnectionEstablishTimeout,
		Arch:                       *arch,
	}

	return &cliArgs, nil
}

// LoadReswarmConfig populates a ReswarmConfig struct from a given path
func LoadReswarmConfig(path string) (*ReswarmConfig, error) {
	jsonFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)

	var reswarmConfig ReswarmConfig
	json.Unmarshal(byteValue, &reswarmConfig)
	return &reswarmConfig, nil
}
