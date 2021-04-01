package system

import (
	"context"
	"errors"
	"fmt"
	"reagent/errdefs"
	"reagent/networkmanager"
	"strings"
	"time"
)

type WiFi struct {
	SSID            string // network name
	MAC             string // MAC
	SecurityType    string
	Signal          uint8 // signal strength (dBm)
	WPAFlags        uint32
	RSNFlags        uint32
	Flags           uint32
	Frequency       uint32 // frequency [MHz]
	Channel         uint32 // channel index
	Current         bool
	Known           bool
	IPv4AddressData []IPv4AddressData
	IPv6AddressData []IPv6AddressData
}

type EthernetDevice struct {
	InterfaceName   string
	MAC             string
	IPv4AddressData []IPv4AddressData
	IPv6AddressData []IPv6AddressData
	Method          string
}

type IPv4AddressData struct {
	Address string
	Prefix  uint8
}

type IPv6AddressData struct {
	Address string
	Prefix  uint8
}

type WiFiCredentials struct {
	Ssid         string
	Passwd       string // password for SSID
	SecurityType string
	Priority     uint32
}

type Network struct {
	nm       networkmanager.NetworkManager
	settings networkmanager.Settings
}

var ErrDeviceNotFound = errors.New("device not found")
var ErrInvalidWiFiPassword = errors.New("the wifi password is invalid")
var ErrNotConnected = errors.New("not connected")

func NewNetwork() (Network, error) {
	nm, err := networkmanager.NewNetworkManager()
	if err != nil {
		return Network{}, err
	}

	settings, err := networkmanager.NewSettings()
	if err != nil {
		return Network{}, err
	}

	return Network{nm, settings}, nil
}

func (n *Network) GetActiveWirelessDevice() (networkmanager.DeviceWireless, error) {
	connections, err := n.nm.GetPropertyActiveConnections()
	if err != nil {
		return nil, err
	}

	for _, connection := range connections {
		devices, err := connection.GetPropertyDevices()
		if err != nil {
			return nil, err
		}

		for _, device := range devices {
			devType, err := device.GetPropertyDeviceType()
			if err != nil {
				return nil, err
			}

			if devType == networkmanager.NmDeviceTypeWifi {
				return networkmanager.NewDeviceWireless(device.GetPath())
			}
		}

	}

	return nil, ErrDeviceNotFound
}

func (n *Network) getAccessPointBySSID(ssid string) (networkmanager.AccessPoint, error) {
	activeWirelessDevice, err := n.GetActiveWirelessDevice()
	if err != nil {
		return nil, err
	}

	accessPoints, err := activeWirelessDevice.GetAllAccessPoints()
	if err != nil {
		return nil, err
	}

	for _, ap := range accessPoints {
		foundSSID, err := ap.GetPropertySSID()
		if err != nil {
			return nil, err
		}

		if foundSSID == ssid {
			return ap, nil
		}
	}

	return nil, errdefs.ErrNotFound
}

func (n *Network) getAccessPointByMAC(mac string) (networkmanager.AccessPoint, error) {
	activeWirelessDevice, err := n.GetActiveWirelessDevice()
	if err != nil {
		return nil, err
	}

	accessPoints, err := activeWirelessDevice.GetAllAccessPoints()
	if err != nil {
		return nil, err
	}

	for _, ap := range accessPoints {
		apMAC, err := ap.GetPropertyHWAddress()
		if err != nil {
			return nil, err
		}

		if apMAC == mac {
			return ap, nil
		}
	}

	return nil, errdefs.ErrNotFound
}

func (n *Network) Reload() error {
	return n.nm.Reload(0)
}

func (n *Network) getConnectionBySSID(ssid string) (networkmanager.Connection, error) {
	savedConnections, err := n.settings.ListConnections()
	if err != nil {
		return nil, err
	}

	for _, connection := range savedConnections {
		settingsMap, err := connection.GetSettings()
		if err != nil {
			return nil, err
		}

		settings := settingsMap["802-11-wireless"]
		ssidArg := settings["ssid"]

		if ssidArg == nil {
			continue
		}

		ssidByteArr, ok := ssidArg.([]byte)
		if !ok {
			return nil, errors.New("failed to parse ssid")
		}

		foundSSID := string(ssidByteArr)

		if ssid == foundSSID {
			return connection, nil
		}
	}

	return nil, errdefs.ErrNotFound
}

func (n *Network) RemoveWifi(ssid string) error {
	connection, err := n.getConnectionBySSID(ssid)
	if err != nil {
		return err
	}

	return connection.Delete()
}

func (n *Network) ActivateWiFi(mac string, ssid string) (networkmanager.ActiveConnection, error) {
	device, err := n.GetActiveWirelessDevice()
	if err != nil {
		return nil, err
	}

	accessPoint, err := n.getAccessPointByMAC(mac)
	if err != nil {
		return nil, err
	}

	connection, err := n.getConnectionBySSID(ssid)
	if err != nil {
		return nil, err
	}

	return n.nm.ActivateWirelessConnection(connection, device, accessPoint)
}

func (n *Network) AddWiFi(mac string, credentials WiFiCredentials) (networkmanager.Connection, error) {
	var ap networkmanager.AccessPoint
	var ssid string
	var secType networkmanager.AccessPointSecurityType
	var err error

	if mac == "" {
		ssid = credentials.Ssid
		secType = networkmanager.AccessPointSecurityType(credentials.SecurityType)
	} else {
		ap, err = n.getAccessPointByMAC(mac)
		if err != nil {
			return nil, err
		}

		ssid, err = ap.GetPropertySSID()
		if err != nil {
			return nil, err
		}

		secType, err = ap.GetSecurityType()
		if err != nil {
			return nil, err
		}
	}

	// see https://developer.gnome.org/NetworkManager/stable/settings-802-11-wireless-security.html
	newConnection := make(networkmanager.ConnectionSettings)
	newConnection["connection"] = make(map[string]interface{})
	newConnection["connection"]["id"] = ssid
	newConnection["connection"]["autoconnect-priority"] = credentials.Priority

	newConnection["802-11-wireless"] = make(map[string]interface{})
	newConnection["802-11-wireless"]["ssid"] = []byte(ssid)

	newConnection["802-11-wireless"]["security"] = "802-11-wireless-security"
	newConnection["802-11-wireless-security"] = make(map[string]interface{})
	newConnection["802-11-wireless-security"]["key-mgmt"] = secType

	if secType == networkmanager.AccessPointSecurityWPA || secType == networkmanager.AccessPointSecurityWPAEnterprise {
		newConnection["802-11-wireless-security"]["psk-flags"] = 0 // Default
		newConnection["802-11-wireless-security"]["psk"] = credentials.Passwd
	} else if secType == networkmanager.AccessPointSecurityWEP {
		newConnection["802-11-wireless-security"]["wep-key-type"] = 1 // NM_WEP_KEY_TYPE_KEY
		newConnection["802-11-wireless-security"]["wep-key0"] = credentials.Passwd
	}

	connection, err := n.settings.AddConnection(newConnection)
	if err != nil {
		if strings.Contains(err.Error(), "802-11-wireless-security.psk: property is invalid") {
			return nil, ErrInvalidWiFiPassword
		}
		if strings.Contains(err.Error(), "802-11-wireless-security.wep-key0: property is invalid") {
			return nil, ErrInvalidWiFiPassword
		}
		return nil, err
	}

	return connection, nil
}

// Scan calls the NetworkManager to see if there's a WiFi device available. If available it will scan, and block until the scan has finished.
// Whenever a scan request gets rate limited, we wait one second and retry until it succeeds. The default scan timeout is 10 seconds.
func (n *Network) Scan(timeoutParam ...time.Duration) error {
	var timeout time.Duration

	if len(timeoutParam) == 0 {
		timeout = time.Second * 10
	} else {
		timeout = timeoutParam[0]
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), timeout)
	defer cancelFunc()

	wirelessDevice, err := n.GetActiveWirelessDevice()
	if err != nil {
		return err
	}

	lastScanBeforeScan, err := wirelessDevice.GetPropertyLastScan()
	if err != nil {
		return err
	}

	err = wirelessDevice.RequestScan()
	if err != nil {
		// we got rate limited, lets retry in one second
		if errors.Is(err, networkmanager.ErrorFollowingPreviousScan) {
			// wait 1 second and then retry the scan
			time.Sleep(time.Second)

			return n.Scan(timeout)
		}
	}

outerLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			lastScan, err := wirelessDevice.GetPropertyLastScan()
			if err != nil {
				return err
			}

			// scan has completed
			if lastScanBeforeScan != lastScan {
				break outerLoop
			}

			devState, err := wirelessDevice.GetPropertyState()
			if err != nil {
				return err
			}

			// the scan won't complete if the device is not activated / managed
			if devState <= networkmanager.NmDeviceStateDisconnected {
				return ErrNotConnected
			}

			time.Sleep(time.Millisecond * 100)
		}
	}

	return nil
}

func (n *Network) isKnown(ssid string, bssid string) (bool, error) {
	savedConnections, err := n.settings.ListConnections()
	if err != nil {
		return false, err
	}

	var allBSSIDs []string
	var allSSIDs []string
	for _, connection := range savedConnections {
		settingsMap, err := connection.GetSettings()
		if err != nil {
			return false, err
		}

		settings := settingsMap["802-11-wireless"]
		ssidArg := settings["ssid"]
		seenBSSIDs := settings["seen-bssids"]

		if ssidArg != nil {
			ssid, ok := ssidArg.([]byte)
			if !ok {
				return false, errors.New("failed to parse ssid")
			}

			allSSIDs = append(allSSIDs, string(ssid))
		}

		// hasn't been actually connected to an AP yet
		if seenBSSIDs == nil {
			continue
		}

		bssids, ok := seenBSSIDs.([]string)
		if !ok {
			return false, errors.New("failed to parse bssids")
		}

		allBSSIDs = append(allBSSIDs, bssids...)
	}

	for _, knownBSSID := range allBSSIDs {
		if knownBSSID == bssid {
			return true, nil
		}
	}

	for _, knownSSID := range allSSIDs {
		if knownSSID == ssid {
			return true, nil
		}
	}

	return false, nil
}

func (n *Network) isCurrent(bssid string) (bool, error) {
	activeWirelessDevice, err := n.GetActiveWirelessDevice()
	if err != nil {
		return false, err
	}

	ac, err := activeWirelessDevice.GetPropertyActiveConnection()
	if err != nil {
		return false, err
	}

	connection, err := ac.GetPropertyConnection()
	if err != nil {
		return false, err
	}

	settingsMap, err := connection.GetSettings()
	if err != nil {
		return false, err
	}

	settings := settingsMap["802-11-wireless"]
	bssids, ok := settings["seen-bssids"].([]string)
	if !ok {
		return false, errors.New("failed to parse bssids")
	}

	for _, currentBssid := range bssids {
		if currentBssid == bssid {
			return true, nil
		}
	}

	return false, nil
}

func (n *Network) getAllConnectionSettings() ([]networkmanager.ConnectionSettings, error) {
	var connectionSettings []networkmanager.ConnectionSettings

	allConnections, err := n.settings.ListConnections()
	if err != nil {
		return nil, err
	}

	for _, connection := range allConnections {
		setting, err := connection.GetSettings()
		if err != nil {
			continue
		}

		connectionSettings = append(connectionSettings, setting)
	}

	return connectionSettings, nil
}

func (n *Network) getEthernetDevices() ([]networkmanager.DeviceWired, error) {
	devices, err := n.nm.GetAllDevices()
	if err != nil {
		return nil, err
	}

	var wiredDevices []networkmanager.DeviceWired
	for _, device := range devices {
		devType, err := device.GetPropertyDeviceType()
		if err != nil {
			return nil, err
		}

		if devType == networkmanager.NmDeviceTypeEthernet {
			wiredDevice, err := networkmanager.NewDeviceWired(device.GetPath())
			if err != nil {
				return nil, err
			}

			wiredDevices = append(wiredDevices, wiredDevice)
		}
	}

	return wiredDevices, nil
}

func (n *Network) accessPointsToWiFi(accessPoints []networkmanager.AccessPoint) ([]WiFi, error) {
	var wifis []WiFi
	for _, ap := range accessPoints {
		wifi := WiFi{}

		ssid, err := ap.GetPropertySSID()
		if err != nil {
			ssid = ""
		}

		wifi.SSID = ssid

		mac, err := ap.GetPropertyHWAddress()
		if err != nil {
			return nil, err
		}

		wifi.MAC = mac

		known, err := n.isKnown(ssid, mac)
		if err != nil {
			return nil, err
		}

		wifi.Known = known

		current, err := n.isCurrent(mac)
		if err != nil {
			return nil, err
		}

		wifi.Current = current

		wpaFlags, err := ap.GetPropertyWPAFlags()
		if err != nil {
			return nil, err
		}

		wifi.WPAFlags = wpaFlags

		signalStrength, err := ap.GetPropertyStrength()
		if err != nil {
			return nil, err
		}

		wifi.Signal = signalStrength

		flags, err := ap.GetPropertyFlags()
		if err != nil {
			return nil, err
		}

		wifi.Flags = flags

		freq, err := ap.GetPropertyFrequency()
		if err != nil {
			return nil, err
		}

		wifi.Frequency = freq

		channel, err := ap.GetChannel()
		if err != nil {
			return nil, err
		}

		wifi.Channel = channel

		rsnFlags, err := ap.GetPropertyRSNFlags()
		if err != nil {
			return nil, err
		}

		wifi.RSNFlags = rsnFlags

		securityType, err := ap.GetSecurityType()
		if err != nil {
			return nil, err
		}

		wifi.SecurityType = string(securityType)

		wifis = append(wifis, wifi)
	}

	return wifis, nil
}

func (n *Network) GetActiveWirelessDeviceConfig() ([]IPv4AddressData, []IPv6AddressData, error) {
	device, err := n.GetActiveWirelessDevice()
	if err != nil {
		return nil, nil, err
	}

	ip4Config, err := device.GetPropertyIP4Config()
	if err != nil {
		return nil, nil, err
	}

	ip6Config, err := device.GetPropertyIP6Config()
	if err != nil {
		return nil, nil, err
	}

	ipv4AddressDatas, err := ip4Config.GetPropertyAddressData()
	if err != nil {
		return nil, nil, err
	}

	var parsedIPv4Datas []IPv4AddressData
	for _, addressData := range ipv4AddressDatas {
		parsedIPv4Datas = append(parsedIPv4Datas, IPv4AddressData(addressData))
	}

	ipv6AddressDatas, err := ip6Config.GetPropertyAddressData()
	if err != nil {
		return nil, nil, err
	}

	var parsedIPv6Datas []IPv6AddressData
	for _, addressData := range ipv6AddressDatas {
		parsedIPv6Datas = append(parsedIPv6Datas, IPv6AddressData(addressData))
	}

	return parsedIPv4Datas, parsedIPv6Datas, nil
}

func (n *Network) updateIPv4Address(device networkmanager.Device, connection networkmanager.Connection, ipAddress string, prefix uint32) error {
	settings, err := connection.GetSettings()
	if err != nil {
		return err
	}

	delete(settings["ipv6"], "addresses")
	delete(settings["ipv6"], "routes")

	if settings["ipv4"] == nil {
		settings["ipv4"] = make(map[string]interface{})
	}

	if settings["ipv4"]["addresses"] != nil {
		delete(settings["ipv4"], "addresses")
	}

	if settings["ipv4"]["address-data"] != nil {
		delete(settings["ipv4"], "address-data")
	}

	if settings["ipv4"]["gateway"] != nil {
		delete(settings["ipv4"], "gateway")
	}

	settings["ipv4"]["method"] = "manual"

	addressData := make([]map[string]interface{}, 1)
	addressData[0] = make(map[string]interface{})
	addressData[0]["address"] = ipAddress
	addressData[0]["prefix"] = prefix

	settings["ipv4"]["address-data"] = addressData

	return connection.Update(settings)
}

func (n *Network) enableDHCP(connection networkmanager.Connection, mac string) error {
	settings, err := connection.GetSettings()
	if err != nil {
		return err
	}

	delete(settings["ipv6"], "addresses")
	delete(settings["ipv6"], "routes")

	settings["ipv4"] = make(map[string]interface{})
	settings["ipv4"]["method"] = "auto"

	return connection.Update(settings)
}

func (n *Network) EnableDHCP(mac string) error {
	devices, err := n.nm.GetAllDevices()
	if err != nil {
		return err
	}

	var ac networkmanager.ActiveConnection
	for _, device := range devices {
		foundMac, err := device.GetPropertyHwAddress()
		if err != nil {
			return err
		}

		if foundMac == mac {
			ac, err = device.GetPropertyActiveConnection()
			if err != nil {
				return err
			}
			break
		}

	}

	if ac == nil {
		return ErrDeviceNotFound
	}

	connection, err := ac.GetPropertyConnection()
	if err != nil {
		return err
	}

	return n.enableDHCP(connection, mac)
}

func (n *Network) SetIPv4Address(mac string, ip string, prefix uint32) error {
	devices, err := n.nm.GetAllDevices()
	if err != nil {
		return err
	}

	var ac networkmanager.ActiveConnection
	var foundDevice networkmanager.Device
	for _, device := range devices {
		foundMac, err := device.GetPropertyHwAddress()
		if err != nil {
			return err
		}

		if foundMac == mac {
			ac, err = device.GetPropertyActiveConnection()
			if err != nil {
				return err
			}

			foundDevice = device
			break
		}

	}

	if ac == nil {
		return ErrDeviceNotFound
	}

	connection, err := ac.GetPropertyConnection()
	if err != nil {
		return err
	}

	return n.updateIPv4Address(foundDevice, connection, ip, prefix)
}

func (n *Network) connectionSettingsToWifi(connectionSettingsMap networkmanager.ConnectionSettings) WiFi {
	var wifi WiFi

	wirelessSettings := connectionSettingsMap["802-11-wireless"]
	wirelessSecurity := connectionSettingsMap["802-11-wireless-security"]
	securityType := fmt.Sprint(wirelessSecurity["key-mgmt"])

	ssidByteArr, ok := wirelessSettings["ssid"].([]byte)
	if !ok {
		ssidByteArr = []byte{}
	}

	ssid := string(ssidByteArr)

	wifi.SSID = ssid
	wifi.Known = true
	wifi.SecurityType = securityType

	return wifi
}

func (n *Network) ListEthernetDevices() ([]EthernetDevice, error) {
	devices, err := n.getEthernetDevices()
	if err != nil {
		return nil, err
	}

	var ethernetDevices []EthernetDevice
	for _, device := range devices {
		var ethernetDevice EthernetDevice
		name, err := device.GetPropertyIpInterface()
		if err != nil {
			return nil, err
		}

		if name == "" {
			name, err = device.GetPropertyInterface()
			if err != nil {
				return nil, err
			}
		}

		ethernetDevice.InterfaceName = name

		ipv4Config, err := device.GetPropertyIP4Config()
		if err != nil {
			return nil, err
		}

		if ipv4Config != nil {
			ipv4AddressDatas, err := ipv4Config.GetPropertyAddressData()
			if err != nil {
				return nil, err
			}

			var parsedIPv4AddressDatas []IPv4AddressData
			for _, ipv4AddressData := range ipv4AddressDatas {
				parsedIPv4AddressDatas = append(parsedIPv4AddressDatas, IPv4AddressData(ipv4AddressData))
			}

			ethernetDevice.IPv4AddressData = parsedIPv4AddressDatas
		}

		ipv6Config, err := device.GetPropertyIP6Config()
		if err != nil {
			return nil, err
		}

		if ipv6Config != nil {
			ipv6AddressDatas, err := ipv6Config.GetPropertyAddressData()
			if err != nil {
				return nil, err
			}

			var parsedIPv6AddressDatas []IPv6AddressData
			for _, ipv6AddressData := range ipv6AddressDatas {
				parsedIPv6AddressDatas = append(parsedIPv6AddressDatas, IPv6AddressData(ipv6AddressData))
			}

			ethernetDevice.IPv6AddressData = parsedIPv6AddressDatas
		}

		mac, err := device.GetPropertyPermHwAddress()
		if err != nil {
			return nil, err
		}

		ethernetDevice.MAC = mac

		ac, err := device.GetPropertyActiveConnection()
		if err != nil {
			return nil, err
		}

		if ac != nil {
			conn, err := ac.GetPropertyConnection()
			if err != nil {
				return nil, err
			}

			if conn != nil {
				settings, err := conn.GetSettings()
				if err != nil {
					return nil, err
				}

				if settings["ipv4"] != nil {
					method := settings["ipv4"]["method"]
					if method != nil {
						ethernetDevice.Method = fmt.Sprint(method)
					}
				}
			}
		}

		ethernetDevices = append(ethernetDevices, ethernetDevice)
	}

	return ethernetDevices, nil
}

func (n *Network) ListWifiNetworks() ([]WiFi, error) {
	activeWirelessDevice, err := n.GetActiveWirelessDevice()
	if err != nil {
		return nil, err
	}

	accessPoints, err := activeWirelessDevice.GetAllAccessPoints()
	if err != nil {
		return nil, err
	}

	wifis, err := n.accessPointsToWiFi(accessPoints)
	if err != nil {
		return nil, err
	}

	allConnectionSettings, err := n.getAllConnectionSettings()
	if err != nil {
		return nil, err
	}

	for _, connectionSettingsMap := range allConnectionSettings {
		settings := connectionSettingsMap["802-11-wireless"]
		ssidByteArr, ok := settings["ssid"].([]byte)
		if !ok {
			continue
		}

		ssid := string(ssidByteArr)

		found := false
		for _, wifi := range wifis {
			if wifi.SSID == ssid {
				found = true
			}
		}

		if !found {
			wifis = append(wifis, n.connectionSettingsToWifi(connectionSettingsMap))
		}
	}

	return wifis, nil
}
