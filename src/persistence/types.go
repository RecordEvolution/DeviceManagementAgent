package persistence

import "reagent/apps"

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

type StateStorer interface {
	Init() // responsible for creating tables etc.
	UpdateAppState(appName string, appKey int, stage apps.Stage, oldState apps.AppState, newState apps.AppState) error
	InsertAppState(appName string, appKey, stage apps.Stage, curState apps.AppState) error
	InsertDeviceState(DeviceStatus, NetworkInterface) error
	UpdateDeviceStatus(DeviceStatus) error
	UpdateNetworkInterface(NetworkInterface) error
	Close()
}
