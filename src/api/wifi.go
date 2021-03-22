package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/messenger"
	"reagent/system"
)

func (ex *External) listWiFiNetworksHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	wifis, err := ex.Network.ListWifiNetworks()
	if err != nil {
		return nil, err
	}

	ipv4, ipv6, err := ex.Network.GetActiveWirelessDeviceConfig()
	if err != nil {
		return nil, err
	}

	// convert to slice to be passed on
	wifiList := make([]interface{}, len(wifis))
	for i, wifi := range wifis {
		wifiList[i] = common.Dict{
			"mac":       wifi.MAC,
			"ssid":      wifi.SSID,
			"channel":   wifi.Channel,
			"signal":    wifi.Signal,
			"security":  wifi.SecurityType,
			"frequency": wifi.Frequency,
			"known":     wifi.Known,
			"current":   wifi.Current,
		}
	}

	return &messenger.InvokeResult{
		Arguments: wifiList,
		ArgumentsKw: common.Dict{
			"ipv4": ipv4,
			"ipv6": ipv6,
		},
	}, nil
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

	return &messenger.InvokeResult{}, ex.Network.RemoveWifi(ssidToRemove)
}

func (ex *External) wifiScanHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	return &messenger.InvokeResult{}, ex.Network.Scan()
}

func (ex *External) wifiRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	return &messenger.InvokeResult{}, ex.Network.Reload()
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

	var mac string
	var securityType string

	ssidArg := wifiDict["ssid"]
	macArg := wifiDict["mac"]
	securityTypeArg := wifiDict["security"]
	passwordArg := wifiDict["password"]
	priorityArg := wifiDict["priority"]

	if macArg != nil {
		mac, ok = macArg.(string)
		if !ok {
			return nil, errors.New("failed to parse mac, invalid type")
		}
	}

	if securityTypeArg != nil {
		securityType, ok = securityTypeArg.(string)
		if !ok {
			return nil, errors.New("failed to parse securityType, invalid type")
		}
	}

	ssid, ok := ssidArg.(string)
	if !ok {
		return nil, errors.New("failed to parse ssid, invalid type")
	}

	password, ok := passwordArg.(string)
	if !ok {
		return nil, errors.New("failed to parse password, invalid type")
	}

	priority, ok := priorityArg.(uint64)
	if !ok {
		return nil, errors.New("failed to parse priority, invalid type")
	}

	wifiEntryPayload := system.WiFiCredentials{
		Ssid:         ssid,
		Passwd:       password,
		Priority:     uint32(priority),
		SecurityType: securityType,
	}

	_, err := ex.Network.AddWiFi(mac, wifiEntryPayload)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}

func (ex *External) selectWiFiNetworkHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	payload := response.Arguments
	if len(payload) == 0 {
		return nil, errors.New("args for add wifi config is empty")
	}

	wifiDict, ok := payload[0].(map[string]interface{})
	if !ok {
		return nil, errors.New("argument 1 of args is not a dictionary type")
	}

	ssidArg := wifiDict["ssid"]
	macArg := wifiDict["mac"]

	ssid, ok := ssidArg.(string)
	if !ok {
		return nil, errors.New("failed to parse ssid, invalid type")
	}

	mac, ok := macArg.(string)
	if !ok {
		return nil, errors.New("failed to parse mac, invalid type")
	}

	_, err := ex.Network.ActivateWiFi(mac, ssid)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
