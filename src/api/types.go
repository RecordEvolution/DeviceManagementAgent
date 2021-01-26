package api

import "context"

type BackendAPI interface {
	GetRequestedAppStates(ctx context.Context, deviceKey int) ([]AppStateResponse, error)
}

type AppStateResponse struct {
	Name                   string      `json:"name"`
	Stage                  string      `json:"stage"`
	Groups                 interface{} `json:"groups"`
	Status                 string      `json:"status"`
	AppKey                 int         `json:"app_key"`
	AppName                string      `json:"app_name"`
	SwarmKey               int         `json:"swarm_key"`
	DeviceKey              int         `json:"device_key"`
	GroupKeys              interface{} `json:"group_keys"`
	Description            string      `json:"description"`
	Environment            interface{} `json:"environment"`
	ReleaseKey             int         `json:"release_key"`
	Architecture           string      `json:"architecture"`
	TargetState            string      `json:"target_state"`
	CurrentState           string      `json:"current_state"`
	SerialNumber           string      `json:"serial_number"`
	ContainerName          string      `json:"container_name"`
	NewImageName           interface{} `json:"new_image_name"`
	NewestVersion          string      `json:"newest_version"`
	RequestUpdate          interface{} `json:"request_update"`
	NewReleaseKey          interface{} `json:"new_release_key"`
	PresentVersion         string      `json:"present_version"`
	InheritFromGroup       interface{} `json:"inherit_from_group"`
	PresentImageName       string      `json:"present_image_name"`
	RequestorAccountKey    int         `json:"requestor_account_key"`
	DeviceOwnerAccountKey  int         `json:"device_owner_account_key"`
	ManuallyRequestedState string      `json:"manually_requested_state"`
}
