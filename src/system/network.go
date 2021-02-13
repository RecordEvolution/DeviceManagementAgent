package system

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/messenger"
	"reagent/messenger/topics"
	// "github.com/theojulienne/go-wireless"
	// "github.com/mdlayher/wifi"
	// "https://github.com/bettercap/bettercap"
)

//  do we have to consider multiple standards or IEEE 802.11n only ??!
type iface struct {
	id  string
	ip  string
	mac string
}

type WiFi struct {
	ssid      string
	passwd    string
	mac       string
	signal    string
	lastseen  string
	frequency string
}

func ListInterfaces() []iface {

	// ifaces := wireless.Interfaces()
	// fmt.Println(ifaces)

	return []iface{}
}

func ListWiFi() []string {
	fmt.Println("starting to list WIFI")

	// wc, err := wireless.NewClient("wlp2s0")
	// defer wc.Close()
	//
	// aps, err := wc.Scan()
	// fmt.Println(aps, err)

	return []string{}
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
	args := []interface{}{common.Dict{
		"swarm_key":  config.ReswarmConfig.SwarmKey,
		"device_key": config.ReswarmConfig.DeviceKey,
		"status":     string(status),
	}}

	_, err := messenger.Call(ctx, topics.UpdateDeviceStatus, args, nil, nil, nil)

	return err
}
