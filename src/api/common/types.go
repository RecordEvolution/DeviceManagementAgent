package common

type App struct {
	Name                   string
	AppKey                 int
	AppName                string
	ManuallyRequestedState AppState
	CurrentState           AppState
	Stage                  Stage
	RequestUpdate          bool
}
