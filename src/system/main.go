package system

import (
	"fmt"
	// "strconv"
	// "reagent/system"
)

func main() {
	ifaces, err := ListNetworkInterfaces()
	if err != nil {
		panic(err)
	}

	var ifaceactive string
	for i, n := range ifaces {
		fmt.Printf("%d: %s\n", i, n.Name)
		fmt.Println(n.Info())
		if n.State == "up" && n.Connected {
			ifaceactive = n.Name
		}
	}

	if ifaceactive != "" {
		fmt.Println("active/connected iface: " + ifaceactive)

		wifis, err := ListWiFiNetworks(ifaceactive)
		if err != nil {
			panic(err)
		}

		for i, n := range wifis {
			fmt.Printf("%d: %s\n", i, n.Ssid)
			fmt.Println(n.Info())
		}
	}

	if true {

		crd := WiFiCredentials{
			Ssid:   "FRITZ!Box 7430 RI",
			Passwd: "R/7z:F%a3b?19cK8xWS5AA",
		}

		err := AddWifiConfig(crd, true)
		if err != nil {
			panic("failed to add WiFi config")
		}

		// wifi := WiFi {
		// 	ssid: "FRITZ!Box 7430 RI lk",
		// 	mac: "e8:df:70:c6:40:bd",
		// }
		//
		// iface := NetworkIface {
		// 	name: "wlp2s0",
		// 	mac: "c8:21:58:e8:eb:94",
		// 	wifi: true,
		// }
		//
		// resa := ActivateWifi(wifi,iface)
		// if !resa {
		// 	panic("failed to active WiFi")
		// }

	}

}
