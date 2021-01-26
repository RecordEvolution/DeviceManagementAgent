package apps

import (
	"reagent/api/common"
	"reagent/system"
)

type StateStorer interface {
	Init() error // responsible for creating tables etc.
	UpdateAppState(app *common.App, newState common.AppState) error
	InsertAppState(app *common.App) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	Close() error
}
