package container

// app states
type AppState int

const (
	PRESENT AppState = iota
	REMOVED
	DOWNLOADING
	DELETING
	RUNNING
	STARTING
	STOPPING
	// ....
)
