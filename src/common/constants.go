package common

import (
	"errors"
)

// AppState states
type AppState string
type Stage string

func IsCancelableState(appState AppState) bool {
	switch appState {
	case BUILDING, PUBLISHING, DOWNLOADING:
		return true
	}
	return false
}

type LogType string

const (
	PULL  LogType = "PULL"
	PUSH  LogType = "PUSH"
	BUILD LogType = "BUILD"
	APP   LogType = "APP"
)

func GetCurrentLogType(currentState AppState) LogType {
	var logType LogType
	if currentState == DOWNLOADING {
		logType = PULL
	} else if currentState == PUBLISHING {
		logType = PUSH
	} else if currentState == BUILDING {
		logType = BUILD
	} else {
		logType = APP
	}
	return logType
}

func TransientToActualState(appState AppState) AppState {
	switch appState {
	case BUILDING,
		TRANSFERED,
		DOWNLOADING,
		TRANSFERING,
		PUBLISHED,
		PUBLISHING,
		UPDATING,
		STOPPING:
		return PRESENT
	case DELETING:
		return REMOVED
	case STARTING:
		return RUNNING
	}

	return appState
}

func IsTransientState(appState AppState) bool {
	switch appState {
	case BUILDING,
		BUILT,
		TRANSFERED,
		DELETING,
		TRANSFERING,
		PUBLISHING,
		PUBLISHED,
		DOWNLOADING,
		STOPPING,
		UPDATING,
		STARTING:
		return true
	}
	return false
}

func ContainerStateToAppState(containerState string, exitCode int) (AppState, error) {
	unknownStateErr := errors.New("unkown state")

	switch containerState {
	case "running":
		return RUNNING, nil
	case "created":
		return PRESENT, nil
	case "removing":
		return STOPPING, nil
	case "paused": // won't occur (as of writing)
		return "", unknownStateErr
	case "restarting":
		return FAILED, nil
	case "exited":
		// 137 = SIGKILL received
		// 0 = Normal exit without error
		// if exitCode == 0 || exitCode == 137 {
		// 	return PRESENT, nil
		// }

		// for now it's more clear to the user, that their container exited if we set it to failed
		return FAILED, nil
	case "dead":
		return FAILED, nil
	}

	return "", unknownStateErr
}

const (
	PRESENT     AppState = "PRESENT"
	REMOVED     AppState = "REMOVED"
	UNINSTALLED AppState = "UNINSTALLED"
	FAILED      AppState = "FAILED"
	BUILT       AppState = "BUILT"
	BUILDING    AppState = "BUILDING"
	TRANSFERED  AppState = "TRANSFERED"
	TRANSFERING AppState = "TRANSFERING"
	PUBLISHING  AppState = "PUBLISHING"
	PUBLISHED   AppState = "PUBLISHED"
	DOWNLOADING AppState = "DOWNLOADING"
	STARTING    AppState = "STARTING"
	STOPPING    AppState = "STOPPING"
	STOPPED     AppState = "STOPPED"
	UPDATING    AppState = "UPDATING"
	DELETING    AppState = "DELETING"
	RUNNING     AppState = "RUNNING"
)

const (
	DEV  Stage = "DEV"
	PROD Stage = "PROD"
)
