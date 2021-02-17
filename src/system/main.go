package system

import (
	"fmt"
	// "strconv"
	// "reagent/system"
)

func main() {

	var ifaces []NetworkIface = ListNetworkInterfaces()
	var ifaceactive string
	for i, n := range ifaces {
		fmt.Printf("%d: %s\n",i,n.name)
		fmt.Println(n.Info())
		if n.state == "up" && n.connected {
			ifaceactive = n.name
		}
	}

	if ifaceactive != "" {
		fmt.Println("active/connected iface: " + ifaceactive)

		var wifis []WiFi = ListWiFiNetworks(ifaceactive)

		for i, n := range wifis {
			fmt.Printf("%d: %s\n",i,n.ssid)
			fmt.Println(n.Info())
		}
	}

	if true {

		crd := WiFiCredentials {
			ssid: "FRITZ!Box 7430 RI",
			passwd: "R/7z:F%a3b?19cK8xWS5AA",
		}

		res := AddWifiConfig(crd,true)
		if res != true {
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
