package api

import (
	"context"
	"errors"
	"reagent/messenger"
	"reagent/system"
)

func (ex *External) listWiFiNetworksHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	// find active network interface
	ifaces, err := system.ListNetworkInterfaces()
	if err != nil {
		return nil, nil
	}

	var ifaceActive system.NetworkIface
	for _, n := range ifaces {
		if n.State == "up" && n.Connected && n.Wifi {
			ifaceActive = n
		}
	}
	if ifaceActive.Name == "" {
		return nil, errors.New("no active WiFi interface available")
	}

	// use active WiFi interface to list networks in range
	wifis, err := system.ListWiFiNetworks(ifaceActive.Name)
	if err != nil {
		return nil, err
	}

	// convert to slice to be passed on
	wifislst := make([]interface{}, len(wifis))
	for idx, netw := range wifis {
		wifislst[idx] = netw.Dict()
	}

	return &messenger.InvokeResult{Arguments: wifislst}, nil
}

func (ex *External) removeWifiHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	payloadArg := response.Arguments
	if len(payloadArg) == 0 {
		return nil, errors.New("args for add wifi config is empty")
	}

	payload, ok := payloadArg[0].(map[string]interface{})
	if !ok {
		return nil, errors.New("argument 1 of args is not a dictionary type")
	}

	ssidToRemove, ok := payload["ssid"].(string)
	if !ok {
		return nil, errors.New("failed to parse ssid, invalid type")
	}

	return &messenger.InvokeResult{}, system.RemoveWifiConfig(ssidToRemove)
}

func (ex *External) wifiRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	err := system.RestartWifi()
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) addWiFiConfigurationHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	payload := response.Arguments
	if len(payload) == 0 {
		return nil, errors.New("args for add wifi config is empty")
	}

	wifiDict, ok := payload[0].(map[string]interface{})
	if !ok {
		return nil, errors.New("argument 1 of args is not a dictionary type")
	}

	ssidArg := wifiDict["ssid"]
	passwordArg := wifiDict["password"]
	countryArg := wifiDict["country"]
	priorityArg := wifiDict["priority"]

	ssid, ok := ssidArg.(string)
	if !ok {
		return nil, errors.New("failed to parse ssid, invalid type")
	}

	password, ok := passwordArg.(string)
	if !ok {
		return nil, errors.New("failed to parse password, invalid type")
	}

	country, ok := countryArg.(string)
	if !ok {
		return nil, errors.New("failed to parse country, invalid type")
	}

	priority, ok := priorityArg.(string)
	if !ok {
		return nil, errors.New("failed to parse priority, invalid type")
	}

	wifiEntryPayload := system.WiFiCredentials{
		Ssid:        ssid,
		Passwd:      password,
		CountryCode: country,
		Priority:    priority,
	}

	err := system.AddWifiConfig(wifiEntryPayload, true)
	if err != nil {
		return nil, errors.New("failed to add WiFi configuration")
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) selectWiFiNetworkHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	args := response.Arguments
	// TODO no idea how the payload format actually looks like!!
	ssid := args[0]

	// retrieve active WiFi interface
	ifaceactive, err := system.GetActiveWiFiInterface()
	if err != nil {
		return nil, err
	}

	// use active WiFi interface to list networks in range and retrieve required one
	wifis, err := system.ListWiFiNetworks(ifaceactive.Name)
	if err != nil {
		return nil, err
	}

	var mywifi system.WiFi
	for _, n := range wifis {
		if n.Ssid == ssid {
			mywifi = n
		}
	}

	if mywifi.Ssid == "" {
		return nil, errors.New("required WiFi SSID not available")
	}

	err = system.ActivateWifi(mywifi, ifaceactive)
	if err != nil {
		return nil, errors.New("failed to select required WiFi")
	}

	return &messenger.InvokeResult{}, nil
}
