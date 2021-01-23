package apps

import (
	"fmt"
	"reagent/fs"
	"reagent/messenger"
)

// State states
type State int

const (
	PRESENT State = iota
	REMOVED
	UNINSTALLED
	FAILED
	BUILDING
	TRANSFERRED
	TRANSFERRING
	PUBLISHING
	DOWNLOADING
	STARTING
	STOPPING
	UPDATING
	DELETING
	RUNNING
)

type Stage int

const (
	DEV Stage = iota
	PROD
)

type TransitionFunc func(TransitionPayload)

type StateMachine struct {
	states     []AppState
	appManager AppManager
	messenger  messenger.Messenger
	config     fs.ReswarmConfig
}

type TransitionPayload struct {
	stage         Stage
	appName       string
	imageName     string
	containerName string
	accountId     int
	registryToken string
}

type AppState struct {
	Name                   string `json:"name"`
	AppKey                 int    `json:"app_key"`
	AppName                string `json:"app_name"`
	ManuallyRequestedState string `json:"manually_requested_state"`
	CurrentState           State
	stage                  Stage
}

func (sm *StateMachine) getTransitionFunc(prevState State, nextState State) TransitionFunc {
	var stateTransitionMap = map[State]map[State]TransitionFunc{
		REMOVED: {
			PRESENT:     nil,
			RUNNING:     nil,
			BUILDING:    sm.buildAppOnDevice,
			PUBLISHING:  nil,
			UNINSTALLED: nil,
		},
		UNINSTALLED: {
			PRESENT:    nil,
			RUNNING:    nil,
			BUILDING:   nil,
			PUBLISHING: nil,
		},
		PRESENT: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
		},
		FAILED: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
			RUNNING:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
		},
		BUILDING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			PUBLISHING:  nil,
		},
		TRANSFERRED: {
			BUILDING:    nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
		},
		TRANSFERRING: {
			REMOVED:     nil,
			UNINSTALLED: nil,
			PRESENT:     nil,
		},
		PUBLISHING: {
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		RUNNING: {
			PRESENT:     nil,
			BUILDING:    nil,
			PUBLISHING:  nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		DOWNLOADING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
		},
		STARTING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		STOPPING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		UPDATING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
		DELETING: {
			PRESENT:     nil,
			REMOVED:     nil,
			UNINSTALLED: nil,
			RUNNING:     nil,
		},
	}

	return stateTransitionMap[prevState][nextState]
}

func (sm *StateMachine) getCurrentState(appName string, stage Stage) *State {
	for _, state := range sm.states {
		if state.AppName == appName && state.stage == stage {
			return &state.CurrentState
		}
	}
	return nil
}

func (sm *StateMachine) setCurrentState(appName string, stage Stage, requestedState State) {
	for _, state := range sm.states {
		if state.AppName == appName && state.stage == stage {
			state.CurrentState = requestedState
		}
	}
}

func (sm *StateMachine) RequestAppState(requestedState State, payload TransitionPayload) {
	currentState := sm.getCurrentState(payload.appName, payload.stage)
	transitionFunc := sm.getTransitionFunc(*currentState, requestedState)
	transitionFunc(payload)
}

func (sm *StateMachine) buildAppOnDevice(payload TransitionPayload) {
	logTerminalTopic := fmt.Sprintf("reswarm.logs.%s.%s", sm.config.SerialNumber, payload.containerName)
	sm.messenger.Publish(logTerminalTopic, []messenger.Dict{{"type": "build", "chunk": "Building image on device ..."}}, nil, nil)
	sm.setCurrentState(payload.appName, payload.stage, BUILDING) // observer observes the state and updates to database

	if payload.stage == DEV {
		err := sm.appManager.BuildDevApp(payload.appName)
		if err != nil {
			sm.setCurrentState(payload.appName, payload.stage, FAILED)
		}
		sm.setCurrentState(payload.appName, payload.stage, PRESENT)
	}

	sm.messenger.Publish(logTerminalTopic, []messenger.Dict{{"type": "build", "chunk": "#################### Image built successfully ####################"}}, nil, nil)
}
