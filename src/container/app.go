package container

// AppState states
type AppState int

const (
	PRESENT AppState = iota
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

var stateTransitionMap = map[AppState]map[AppState]func(){
	REMOVED: {
		PRESENT:     func() {},
		RUNNING:     func() {},
		BUILDING:    func() {},
		PUBLISHING:  func() {},
		UNINSTALLED: func() {},
	},
	UNINSTALLED: {
		PRESENT:    func() {},
		RUNNING:    func() {},
		BUILDING:   func() {},
		PUBLISHING: func() {},
	},
	PRESENT: {
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		RUNNING:     func() {},
		BUILDING:    func() {},
		PUBLISHING:  func() {},
	},
	FAILED: {
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		PRESENT:     func() {},
		RUNNING:     func() {},
		BUILDING:    func() {},
		PUBLISHING:  func() {},
	},
	BUILDING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		PUBLISHING:  func() {},
	},
	TRANSFERRED: {
		BUILDING:    func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		PRESENT:     func() {},
	},
	TRANSFERRING: {
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		PRESENT:     func() {},
	},
	PUBLISHING: {
		REMOVED:     func() {},
		UNINSTALLED: func() {},
	},
	RUNNING: {
		PRESENT:     func() {},
		BUILDING:    func() {},
		PUBLISHING:  func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
	},
	DOWNLOADING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
	},
	STARTING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		RUNNING:     func() {},
	},
	STOPPING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		RUNNING:     func() {},
	},
	UPDATING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		RUNNING:     func() {},
	},
	DELETING: {
		PRESENT:     func() {},
		REMOVED:     func() {},
		UNINSTALLED: func() {},
		RUNNING:     func() {},
	},
}

func GetTransitionFunc(prevState AppState, nextState AppState) func() {
	return stateTransitionMap[prevState][nextState]
}
