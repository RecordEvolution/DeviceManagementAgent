package persistence

import (
	"reagent/common"
	"reagent/system"
)

type PersistentAppState struct {
	AppName   string
	AppKey    int
	Stage     common.Stage
	State     common.AppState
	Timestamp string
}

type StateStorer interface {
	Init() error // responsible for creating tables etc.
	UpdateAppState(app *common.App, newState common.AppState) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	UpsertRequestedStateChange(payload common.TransitionPayload) error
	BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error
	GetAppState(appKey uint64, stage common.Stage) (PersistentAppState, error)
	GetAppStates() ([]PersistentAppState, error)
	GetRequestedState(app *common.App) (common.TransitionPayload, error)
	GetRequestedStates() ([]common.TransitionPayload, error)
	Close() error
}
