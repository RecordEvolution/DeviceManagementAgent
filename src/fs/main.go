package fs

import (
	"encoding/json"
	"io/ioutil"
	"log"
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
	Description    interface{} `json:"description"`
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

func LoadReswarmConfig(path string) ReswarmConfig {
	jsonFile, err := os.Open(path)
	if err != nil {
		log.Println("Failed to load file", path)
	}

	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)

	var reswarmConfig ReswarmConfig
	json.Unmarshal(byteValue, &reswarmConfig)
	return reswarmConfig
}
