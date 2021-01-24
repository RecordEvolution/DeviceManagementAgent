package apps

import "reagent/system"

type StateStorer interface {
	Init() // responsible for creating tables etc.
	UpdateAppState(app *App, newState AppState) error
	InsertAppState(app *App) error
	InsertDeviceState(system.DeviceStatus, system.NetworkInterface) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	Close()
}
