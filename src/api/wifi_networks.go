package api

import (
	"context"
	"errors"
	// "reagent/common"
	"reagent/messenger"
  "reagent/system"
)

func (ex *External) listWiFiNetworksHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

  // find active network interface
  var ifaces []system.NetworkIface = system.ListNetworkInterfaces()
	var ifaceactive system.NetworkIface
	for _, n := range ifaces {
		if n.State == "up" && n.Connected && n.Wifi {
			ifaceactive = n
		}
	}
  if ifaceactive.Name == "" {
    return nil, errors.New("no active WiFi interface available")
  }

  // use active WiFi interface to list networks in range
  var wifis []system.WiFi = system.ListWiFiNetworks(ifaceactive.Name)

	// convert to slice to be passed on
	wifislst := make([]interface{}, len(wifis))
	for idx, netw := range wifis {
		wifislst[idx] = netw.Dict()
	}

	return &messenger.InvokeResult{Arguments: wifislst}, nil
}

func (ex *External) addWiFiConfigurationHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

  // args := response.Arguments
	// TODO no idea how the payload format actually looks like!!
	ssid := "myssid" // args[0]
	passwd := "mypassword" //args[1]

	res := system.AddWifiConfig(system.WiFiCredentials{Ssid:ssid,Passwd:passwd},true)
	if !res {
		return nil, errors.New("failed to add WiFi configuration")
	}

  return &messenger.InvokeResult{}, nil
}

func (ex *External) selectWiFiNetworkHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {

	args := response.Arguments
	// TODO no idea how the payload format actually looks like!!
	ssid := args[0]

	// retrieve active WiFi interface
	ifaceactive := system.GetActiveWiFiInterface()

	// use active WiFi interface to list networks in range and retrieve required one
  var wifis []system.WiFi = system.ListWiFiNetworks(ifaceactive.Name)
	var mywifi system.WiFi
	for _, n := range wifis {
		if n.Ssid == ssid  {
			mywifi = n
		}
	}
  if mywifi.Ssid == "" {
    return nil, errors.New("required WiFi SSID not available")
  }

	res := system.ActivateWifi(mywifi,ifaceactive)
	if !res {
		return nil, errors.New("failed to select required WiFi")
	}

  return &messenger.InvokeResult{}, nil
}
