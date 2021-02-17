package system

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
)

// ------------------------------------------------------------------------- //

func UpdateRemoteDeviceStatus(messenger messenger.Messenger, status DeviceStatus) error {
	config := messenger.GetConfig()
	ctx := context.Background()
	args := []interface{}{common.Dict{
		"swarm_key":  config.ReswarmConfig.SwarmKey,
		"device_key": config.ReswarmConfig.DeviceKey,
		"status":     string(status),
	}}

	_, err := messenger.Call(ctx, topics.UpdateDeviceStatus, args, nil, nil, nil)

	return err
}

// ------------------------------------------------------------------------- //
