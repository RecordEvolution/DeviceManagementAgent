package common

import (
	"errors"
	"reagent/config"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type Dict map[string]interface{}
type Timestamp string

type App struct {
	AppKey              uint64
	DeviceToAppKey      uint64
	RequestorAccountKey uint64
	ReleaseKey          uint64
	AppName             string
	RequestedState      AppState
	CurrentState        AppState
	UpdateStatus        UpdateStatus
	Stage               Stage
	RequestUpdate       bool
	ReleaseBuild        bool
	Version             string
	LastUpdated         Timestamp
	TransitionLock      *semaphore.Weighted
	StateLock           sync.Mutex
}

func (app *App) SecureTransition() bool {
	if app.TransitionLock == nil {
		log.Error().Err(errors.New("no semaphore initialized"))
		return false
	}
	return !app.TransitionLock.TryAcquire(1)
}

func (app *App) UnlockTransition() {
	app.TransitionLock.Release(1)
}

func (app *App) IsCancelable() bool {
	app.StateLock.Lock()
	currAppState := app.CurrentState
	app.StateLock.Unlock()

	return IsCancelableState(currAppState)
}

type StageBasedResult struct {
	Dev  string
	Prod string
}

type DockerCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PortForwardRule struct {
	Main                  bool   `json:"main"`
	RuleName              string `json:"name"`
	Active                bool   `json:"active"`
	Public                bool   `json:"public"`
	Port                  uint64 `json:"port"`
	Protocol              string `json:"protocol"`
	LocalIP               string `json:"local_ip"`
	RemotePortEnvironment string `json:"remote_port_environment"`
	RemotePort            uint64 `json:"remote_port"`
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
	DockerCredentials    map[string]DockerCredential
	EnvironmentVariables map[string]any
	EnvironmentTemplate  map[string]any
	DockerCompose        map[string]any
	NewDockerCompose     map[string]any
	Ports                []any
	PublishContainerName string
	RegisteryToken       string
	NewestVersion        string
	PresentVersion       string
	ReleaseKey           uint64
	NewReleaseKey        uint64
	Version              string
	RequestUpdate        bool
	Retrying             bool
	CancelTransition     bool
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
	EnvironmentTemplate    map[string]interface{} `json:"environment_template"`
	DockerCompose          map[string]interface{} `json:"docker_compose"`
	NewDockerCompose       map[string]interface{} `json:"new_docker_compose"`
	Ports                  []interface{}          `json:"ports"`
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
