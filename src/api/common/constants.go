package common

// AppState states
type AppState string

const (
	PRESENT      AppState = "PRESENT"
	REMOVED      AppState = "REMOVED"
	UNINSTALLED  AppState = "UNINSTALLED"
	FAILED       AppState = "FAILED"
	BUILDING     AppState = "BUILDING"
	TRANSFERRED  AppState = "TRANSFERRED"
	TRANSFERRING AppState = "TRANSFERRING"
	PUBLISHING   AppState = "PUBLISHING"
	DOWNLOADING  AppState = "DOWNLOADING"
	STARTING     AppState = "STARTING"
	STOPPING     AppState = "STOPPING"
	UPDATING     AppState = "UPDATING"
	DELETING     AppState = "DELETING"
	RUNNING      AppState = "RUNNING"
)

type Stage string

const (
	DEV  Stage = "DEV"
	PROD Stage = "PROD"
)
