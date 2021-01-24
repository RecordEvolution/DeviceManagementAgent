package persistence

import "reagent/apps"

type DeviceState string
type NetworkInterface string

const (
	CONNECTED    DeviceState = "CONNECTED"
	DISCONNECTED DeviceState = "DISCONNECTED"
)

const (
	WLAN     NetworkInterface = "WLAN"
	ETHERNET NetworkInterface = "ETHERNET"
	NONE     NetworkInterface = "NONE"
)

type StateStorer interface {
	Init() // responsible for creating tables etc.
	PersistAppState(appName string, appKey int, stage apps.Stage, curState apps.AppState, reqState apps.AppState) error
	PersistDeviceState(curState DeviceState, reqState DeviceState, curInt NetworkInterface, reqInt NetworkInterface) error
	Close()
}
