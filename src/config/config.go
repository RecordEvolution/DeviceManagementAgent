package config

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
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
	AppBuildsDirectory       string
	CompressedBuildExtension string
	Debug                    bool
	DebugMessaging           bool
	LogFileLocation          string
	ConfigFileLocation       string
}

type Config struct {
	ReswarmConfig        *ReswarmConfig
	CommandLineArguments *CommandLineArguments
}

func GetCliArguments() *CommandLineArguments {
	logFile := flag.String("logFile", "/Users/ruben/Desktop/reagent.log",
		"Log file used by the ReAgent to store all its log messages")
	debug := flag.Bool("debug", false, "sets the log level to debug")
	debugMessaging := flag.Bool("debugMessaging", false, "enables debug logs for messenger (e.g. WAMP messages)")
	appsBuildDirectory := flag.String("appsDirectory", "/Users/ruben/Desktop", "sets the directory where app build files will be stored")
	compressedBuildExtension := flag.String("compressedBuildExtension", ".tgz", "sets the extension used to decompress the transfered build files")
	cfgFile := flag.String("config", "./demo_demo_swarm_TestDevice.reswarm",
		"Configuration file of IoT device running on localhost")

	flag.Parse()

	cliArgs := CommandLineArguments{
		AppBuildsDirectory:       *appsBuildDirectory,
		CompressedBuildExtension: *compressedBuildExtension,
		Debug:                    *debug,
		DebugMessaging:           *debugMessaging,
		LogFileLocation:          *logFile,
		ConfigFileLocation:       *cfgFile,
	}

	return &cliArgs
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
