package system

import (
	"context"
	"reagent/common"
	"reagent/messenger"
)

type WiFi struct {
	ssid   string
	passwd string
}

func ListWiFi() []string {
	return []string{"RecordEvolution2GHz"}
}

func SetWifi(ssid string) bool {
	return true
}

func AdjustSettings(config string) bool {
	return true
}

func UpdateRemoteDeviceStatus(messenger messenger.Messenger, status DeviceStatus) error {
	config := messenger.GetConfig()
	ctx := context.Background()
	args := []common.Dict{{
		"swarm_key":  config.ReswarmConfig.SwarmKey,
		"device_key": config.ReswarmConfig.DeviceKey,
		"status":     string(status),
	}}

	_, err := messenger.Call(ctx, "reswarm.devices.update_device", args, nil, nil, nil)

	return err
}
