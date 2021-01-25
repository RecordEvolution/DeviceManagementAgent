package apps

import "reagent/system"

type StateStorer interface {
	Init() error // responsible for creating tables etc.
	UpdateAppState(app *App, newState AppState) error
	InsertAppState(app *App) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	Close() error
}
