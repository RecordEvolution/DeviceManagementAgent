package system

type DeviceStatus string
type NetworkInterface string

const (
	CONNECTED    DeviceStatus = "CONNECTED"
	DISCONNECTED DeviceStatus = "DISCONNECTED"
)

const (
	WLAN     NetworkInterface = "WLAN"
	ETHERNET NetworkInterface = "ETHERNET"
	NONE     NetworkInterface = "NONE"
)
