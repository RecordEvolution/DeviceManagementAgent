package persistence

import "reagent/apps"

type DeviceState int
type NetworkState int
type NetworkInterface int

const (
	CONNECTED DeviceState = iota
	DISCONNECTED
)

const (
	WLAN NetworkInterface = iota
	ETHERNET
	NONE
)

type StateStorer interface {
	Init() // responsible for creating tables etc.
	PersistAppState(appName string, appKey int, stage apps.Stage, curState apps.AppState, reqState apps.AppState)
	PersistDeviceState(curState DeviceState, reqState DeviceState, curInt NetworkInterface, reqInt NetworkInterface)
	Close()
}
