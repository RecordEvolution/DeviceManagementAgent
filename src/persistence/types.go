package persistence

import (
	"reagent/common"
	"reagent/messenger"
	"reagent/system"
)

type Database interface {
	Init() error // responsible for creating tables etc.
	UpsertAppState(app *common.App, newState common.AppState) (common.Timestamp, error)
	UpdateDeviceStatus(messenger.DeviceStatus) error
	UpdateNetworkInterface(system.NetworkInterface) error
	UpsertRequestedStateChange(payload common.TransitionPayload) error
	BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error
	GetAppLogHistory(appName string, appKey uint64, stage common.Stage) ([]string, error)
	UpsertLogHistory(appName string, appKey uint64, stage common.Stage, logs []string) error
	ClearAllLogHistory(appName string, appKey uint64, stage common.Stage) error
	GetAppState(appKey uint64, stage common.Stage) (*common.App, error)
	GetAppStates() ([]*common.App, error)
	GetRequestedState(aKey uint64, aStage common.Stage) (common.TransitionPayload, error)
	GetRequestedStates() ([]common.TransitionPayload, error)
	QueueTask(task func())
	Close() error
}
