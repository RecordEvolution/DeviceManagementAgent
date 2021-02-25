package config

import (
	"encoding/json"
	"errors"
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
}

type CommandLineArguments struct {
	AppsDirectory            string
	AppsBuildDir             string
	AppsSharedDir            string
	AgentDir                 string
	AgentDownloadDir         string
	CompressedBuildExtension string
	RemoteUpdateURL          string
	Debug                    bool
	DebugMessaging           bool
	LogFileLocation          string
	ConfigFileLocation       string
	DatabaseFileName         string
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

	appsDir := defaultAgentDir + "/apps"

	logFile := flag.String("logFile", defaultLogFilePath, "Log file used by the ReAgent to store all its log messages")
	debug := flag.Bool("debug", true, "sets the log level to debug")
	remoteUpdateURL := flag.String("remoteUpdateURL", "https://storage.googleapis.com/re-agent", "used to download new versions of the agent")
	agentDir := flag.String("agentDir", defaultAgentDir, "default location of the agent binary")
	databaseFileName := flag.String("dbFileName", "reagent.db", "defines the name used to persist the database file")
	debugMessaging := flag.Bool("debugMessaging", false, "enables debug logs for messenger (e.g. WAMP messages)")
	compressedBuildExtension := flag.String("compressedBuildExtension", "tgz", "sets the extension in which the compressed build files will be provided")
	cfgFile := flag.String("config", "", "reswarm configuration file")
	flag.Parse()

	if *cfgFile == "" {
		return nil, errors.New("the config file path cannot be empty")
	}

	cliArgs := CommandLineArguments{
		AppsDirectory:            appsDir,
		AppsBuildDir:             (appsDir) + "/build",
		AppsSharedDir:            (appsDir) + "/shared",
		AgentDownloadDir:         (*agentDir) + "/downloads",
		AgentDir:                 *agentDir,
		RemoteUpdateURL:          *remoteUpdateURL,
		CompressedBuildExtension: *compressedBuildExtension,
		Debug:                    *debug,
		DebugMessaging:           *debugMessaging,
		LogFileLocation:          *logFile,
		ConfigFileLocation:       *cfgFile,
		DatabaseFileName:         *databaseFileName,
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
