package system

import (
	"errors"
	"os"
	"reagent/common"

	"github.com/rs/zerolog/log"
)

type DeviceConfig struct {
	BootConfig        string
	Cmdline           string
	NetworkInterfaces string
}

const (
	bootConfigPath        = "/boot/config.txt"  // Raspi only
	cmdlineConfigPath     = "/boot/cmdline.txt" // Raspi only
	networkInterfacesPath = "/etc/network/interfaces"
)

// ------------------------------------------------------------------------- //

func UpdateDeviceConfig(oldDevConf *DeviceConfig, newDevConf *DeviceConfig) (bool, error) {
	// set to true when we have overwritten any of the device config files
	hasUpdated := false

	log.Debug().Msg(oldDevConf.BootConfig)
	log.Debug().Msg(newDevConf.BootConfig)
	if oldDevConf.BootConfig != newDevConf.BootConfig {
		// err := filesystem.OverwriteFile(bootConfigPath, newDevConf.BootConfig)
		// if err != nil {
		// 	return false, err
		// }

		hasUpdated = true
	}

	log.Debug().Msg(oldDevConf.Cmdline)
	log.Debug().Msg(newDevConf.Cmdline)
	if oldDevConf.Cmdline != newDevConf.Cmdline {
		// err := filesystem.OverwriteFile(cmdlineConfigPath, newDevConf.Cmdline)
		// if err != nil {
		// 	return false, err
		// }

		hasUpdated = true
	}

	log.Debug().Msg(oldDevConf.NetworkInterfaces)
	log.Debug().Msg(newDevConf.NetworkInterfaces)
	if oldDevConf.NetworkInterfaces != newDevConf.NetworkInterfaces {
		// err := filesystem.OverwriteFile(networkInterfacesPath, newDevConf.NetworkInterfaces)
		// if err != nil {
		// 	return false, err
		// }
		hasUpdated = true
	}

	return hasUpdated, nil
}

func getDeviceConfig() (DeviceConfig, error) {
	bootConfig, err := os.ReadFile(bootConfigPath)
	if err != nil {
		return DeviceConfig{}, err
	}

	cmdline, err := os.ReadFile(cmdlineConfigPath)
	if err != nil {
		return DeviceConfig{}, err
	}

	networkInterfaces, err := os.ReadFile(networkInterfacesPath)
	if err != nil {
		return DeviceConfig{}, err
	}

	return DeviceConfig{
		BootConfig:        string(bootConfig),
		Cmdline:           string(cmdline),
		NetworkInterfaces: string(networkInterfaces),
	}, nil
}

func buildDeviceConfigFromPayload(result *common.Result) (DeviceConfig, error) {
	devInfoPayloadArgs := result.Arguments[0]
	secondArray, ok := (devInfoPayloadArgs).([]interface{})
	if !ok {
		return DeviceConfig{}, errors.New("failed to parse payload dict")
	}

	devInfoPayload, ok := secondArray[0].(map[string]interface{})
	if !ok {
		return DeviceConfig{}, errors.New("failed to parse payload dict")
	}

	newBootConfig, ok := devInfoPayload["boot_config"].(string)
	if !ok {
		return DeviceConfig{}, errors.New("failed to parse 'boot_config' argument")
	}

	newCmdline, ok := devInfoPayload["cmdline"].(string)
	if !ok {
		return DeviceConfig{}, errors.New("failed to parse 'cmdline' argument")
	}

	newNetworkInterfaces, ok := devInfoPayload["network_interfaces"].(string)
	if !ok {
		return DeviceConfig{}, errors.New("failed to parse 'network_interfaces' argument")
	}

	return DeviceConfig{
		BootConfig:        newBootConfig,
		Cmdline:           newCmdline,
		NetworkInterfaces: newNetworkInterfaces,
	}, nil
}

// ------------------------------------------------------------------------- //
