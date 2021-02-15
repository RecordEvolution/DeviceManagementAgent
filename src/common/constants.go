package common

// AppState states
type AppState string
type Stage string

var CancelableTransitions = []AppState{BUILDING, PUBLISHING, DOWNLOADING}

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
