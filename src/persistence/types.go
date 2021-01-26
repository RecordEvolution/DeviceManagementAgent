package persistence

import (
	"reagent/api/common"
	"reagent/system"
)

type PersistentAppState struct {
	ID        int
	AppName   string
	AppKey    int
	Stage     common.Stage
	State     common.AppState
	Timestamp string
}

type Storer interface {
	Init() error // responsible for creating tables etc.
	UpdateAppState(app *common.App, newState common.AppState) error
	InsertAppState(app *common.App) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	GetLocalAppStates() ([]PersistentAppState, error)
	Close() error
}
