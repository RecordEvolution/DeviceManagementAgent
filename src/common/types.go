package common

import (
	"errors"
	"reagent/config"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type Dict map[string]interface{}
type App struct {
	AppKey              uint64
	DeviceToAppKey      uint64
	RequestorAccountKey uint64
	ReleaseKey          uint64
	AppName             string
	RequestedState      AppState
	CurrentState        AppState
	Stage               Stage
	RequestUpdate       bool
	ReleaseBuild        bool
	Version             string
	Semaphore           *semaphore.Weighted
}

func (app *App) SecureLock() bool {
	if app.Semaphore == nil {
		log.Error().Err(errors.New("no semaphore initialized"))
		return false
	}
	return !app.Semaphore.TryAcquire(1)
}

func (app *App) Unlock() {
	app.Semaphore.Release(1)
}

func (app *App) IsCancelable() bool {
	for _, transition := range CancelableTransitions {
		if app.CurrentState == transition {
			return true
		}
	}
	return false
}

type StageBasedResult struct {
	Dev  string
	Prod string
}

// TransitionPayload provides the data used by the StateMachine to transition between states.
type TransitionPayload struct {
	RequestedState       AppState
	CurrentState         AppState
	Stage                Stage
	RequestorAccountKey  uint64
	DeviceToAppKey       uint64
	AppKey               uint64
	CallerAuthID         int
	AppName              string
	ImageName            StageBasedResult
	PresentImageName     string
	RegistryImageName    StageBasedResult
	ContainerName        StageBasedResult
	EnvironmentVariables map[string]interface{}
	PublishContainerName string
	RegisteryToken       string
	NewestVersion        string
	PresentVersion       string
	ReleaseKey           uint64
	NewReleaseKey        uint64
	Version              string
	RequestUpdate        bool
}

func BuildTransitionPayload(appKey uint64, appName string, requestorAccountKey uint64,
	stage Stage, currentState AppState, requestedState AppState, releaseKey uint64, newReleaseKey uint64,
	config *config.Config,
) TransitionPayload {

	payload := TransitionPayload{
		Stage:               stage,
		RequestedState:      requestedState,
		AppName:             appName,
		AppKey:              appKey,
		CurrentState:        currentState,
		ReleaseKey:          releaseKey,
		NewReleaseKey:       newReleaseKey,
		RequestorAccountKey: requestorAccountKey,
	}

	payload.initContainerData(appKey, appName, config)

	return payload
}

type DeviceSyncResponse struct {
	DeviceKey              int                    `json:"device_key"`
	SwarmKey               int                    `json:"swarm_key"`
	AccountKey             int                    `json:"account_key"`
	SerialNumber           string                 `json:"serial_number"`
	AppName                string                 `json:"app_name"`
	Name                   string                 `json:"name"` // device name
	Status                 string                 `json:"status"`
	Architecture           string                 `json:"architecture"`
	Address                string                 `json:"address"`
	ReleaseKey             int                    `json:"release_key"`
	NewReleaseKey          int                    `json:"new_release_key"`
	DeviceToAppKey         int                    `json:"device_to_app_key"`
	Environment            map[string]interface{} `json:"environment"`
	CurrentState           string                 `json:"current_state"`
	Stage                  string                 `json:"stage"`
	ContainerName          string                 `json:"container_name"`
	RequestUpdate          bool                   `json:"request_update"`
	TargetState            string                 `json:"target_state"`
	ManuallyRequestedState string                 `json:"manually_requested_state"`
	InheritFromGroup       interface{}            `json:"inherit_from_group"`
	RequestorAccountKey    int                    `json:"requestor_account_key"`
	PresentVersion         string                 `json:"present_version"`
	PresentImageName       string                 `json:"present_image_name"`
	NewImageName           string                 `json:"new_image_name"`
	NewestVersion          string                 `json:"newest_version"`
	Description            string                 `json:"description"`
	AppKey                 uint64                 `json:"app_key"`
}

// type AppStateResponse struct {
// 	Name                   string      `json:"name"`
// 	Stage                  string      `json:"stage"`
// 	Groups                 interface{} `json:"groups"`
// 	Status                 string      `json:"status"`
// 	AppKey                 int         `json:"app_key"`
// 	AppName                string      `json:"app_name"`
// 	SwarmKey               int         `json:"swarm_key"`
// 	DeviceKey              int         `json:"device_key"`
// 	GroupKeys              interface{} `json:"group_keys"`
// 	Description            string      `json:"description"`
// 	Environment            interface{} `json:"environment"`
// 	ReleaseKey             int         `json:"release_key"`
// 	Architecture           string      `json:"architecture"`
// 	TargetState            string      `json:"target_state"`
// 	CurrentState           string      `json:"current_state"`
// 	SerialNumber           string      `json:"serial_number"`
// 	ContainerName          string      `json:"container_name"`
// 	NewImageName           interface{} `json:"new_image_name"`
// 	NewestVersion          string      `json:"newest_version"`
// 	RequestUpdate          interface{} `json:"request_update"`
// 	NewReleaseKey          interface{} `json:"new_release_key"`
// 	PresentVersion         string      `json:"present_version"`
// 	InheritFromGroup       interface{} `json:"inherit_from_group"`
// 	PresentImageName       string      `json:"present_image_name"`
// 	RequestorAccountKey    int         `json:"requestor_account_key"`
// 	DeviceOwnerAccountKey  int         `json:"device_owner_account_key"`
// 	ManuallyRequestedState string      `json:"manually_requested_state"`
// }
