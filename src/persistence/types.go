package persistence

import (
	"reagent/common"
	"reagent/system"
)

type Database interface {
	Init() error // responsible for creating tables etc.
	UpsertAppState(app *common.App, newState common.AppState) (common.Timestamp, error)
	UpdateDeviceStatus(system.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	UpsertRequestedStateChange(payload common.TransitionPayload) error
	BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error
	GetAppState(appKey uint64, stage common.Stage) (*common.App, error)
	GetAppStates() ([]*common.App, error)
	GetRequestedState(aKey uint64, aStage common.Stage) (common.TransitionPayload, error)
	GetRequestedStates() ([]common.TransitionPayload, error)
	Close() error
}
