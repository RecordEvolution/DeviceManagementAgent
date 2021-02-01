package common

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
}

// common.TransitionPayload provides the data used by the StateMachine to transition between states.
type TransitionPayload struct {
	RequestedState      AppState
	CurrentState        AppState
	Stage               Stage
	RequestorAccountKey uint64
	DeviceToAppKey      uint64
	AppKey              uint64
	AppName             string
	ImageName           string
	NewImageName        string
	PresentImageName    string
	RepositoryImageName string
	ContainerName       string
	AccountID           string
	RegisteryToken      string
	PresentVersion      string
	RequestUpdate       bool
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
