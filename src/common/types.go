package common

import "reagent/config"

type Dict map[string]interface{}

type App struct {
	AppKey                 uint64
	DeviceToAppKey         uint64
	RequestorAccountKey    uint64
	AppName                string
	ManuallyRequestedState AppState
	CurrentState           AppState
	Stage                  Stage
	RequestUpdate          bool
	ReleaseBuild           bool
	transitioning          bool
}

func (a *App) IsTransitioning() bool {
	return a.transitioning
}

func (a *App) BeginTransition() {
	a.transitioning = true
}

func (a *App) FinishTransition() {
	if a.transitioning {
		a.transitioning = false
	}
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
	PublishContainerName string
	RegisteryToken       string
	NewestVersion        string
	PresentVersion       string
	Version              string
	RequestUpdate        bool
}

func BuildTransitionPayload(appKey uint64, appName string, requestorAccountKey uint64,
	stage Stage, currentState AppState, requestedState AppState,
	config *config.Config,
) TransitionPayload {

	payload := TransitionPayload{
		Stage:               stage,
		RequestedState:      requestedState,
		AppName:             appName,
		AppKey:              appKey,
		CurrentState:        currentState,
		RequestorAccountKey: requestorAccountKey,
	}

	payload.initContainerData(appKey, appName, config)

	return payload
}

type DeviceSyncResponse struct {
	DeviceKey              int         `json:"device_key"`
	SwarmKey               int         `json:"swarm_key"`
	AccountKey             int         `json:"account_key"`
	SerialNumber           string      `json:"serial_number"`
	Name                   string      `json:"name"`
	Status                 string      `json:"status"`
	Architecture           string      `json:"architecture"`
	Address                string      `json:"address"`
	ReleaseKey             int         `json:"release_key"`
	DeviceToAppKey         int         `json:"device_to_app_key"`
	CurrentState           string      `json:"current_state"`
	Stage                  string      `json:"stage"`
	ContainerName          string      `json:"container_name"`
	RequestUpdate          bool        `json:"request_update"`
	ManuallyRequestedState string      `json:"manually_requested_state"`
	InheritFromGroup       interface{} `json:"inherit_from_group"`
	RequestorAccountKey    int         `json:"requestor_account_key"`
	PresentVersion         string      `json:"present_version"`
	PresentImageName       string      `json:"present_image_name"`
	NewImageName           string      `json:"new_image_name"`
	NewestVersion          string      `json:"newest_version"`
	Description            string      `json:"description"`
	AppKey                 uint64      `json:"app_key"`
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
