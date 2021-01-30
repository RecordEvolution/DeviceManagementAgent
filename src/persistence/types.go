package persistence

import (
	"reagent/common"
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

type StateStorer interface {
	Init() error // responsible for creating tables etc.
	UpdateLocalAppState(app *common.App, newState common.AppState) error
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	UpsertRequestedStateChange(payload common.TransitionPayload) error
	BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error
	GetLocalAppStates() ([]PersistentAppState, error)
	GetLocalRequestedStates() ([]common.TransitionPayload, error)
	Close() error
}
