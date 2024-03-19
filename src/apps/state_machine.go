package apps

import (
	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/safe"

	"github.com/rs/zerolog/log"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver *StateObserver
	Filesystem    *filesystem.Filesystem
	Container     container.Container
	LogManager    *logging.LogManager
	appStates     []*common.App
}

func NewStateMachine(container container.Container, logManager *logging.LogManager, observer *StateObserver, filesystem *filesystem.Filesystem) StateMachine {
	appStates := make([]*common.App, 0)
	return StateMachine{
		StateObserver: observer,
		Container:     container,
		LogManager:    logManager,
		Filesystem:    filesystem,
		appStates:     appStates,
	}
}

func (sm *StateMachine) noActionTransitionFunc(TransitionPayload common.TransitionPayload, app *common.App) error {
	return errdefs.NoActionTransition()
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.removedToPresent,
			common.RUNNING:     sm.removedToRunning,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.REMOVED:     sm.noActionTransitionFunc,
		},
		common.UNINSTALLED: {
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.UNINSTALLED: sm.noActionTransitionFunc,
		},
		common.PUBLISHING: {
			common.REMOVED: sm.cancelPush,
		},
		common.BUILDING: {
			common.REMOVED: sm.cancelBuild,
		},
		common.STOPPED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.STOPPED:     sm.noActionTransitionFunc,
		},
		common.PRESENT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.PRESENT:     sm.noActionTransitionFunc,
		},
		common.FAILED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.recoverFailToPresentHandler,
			common.RUNNING:     sm.recoverFailToRunningHandler,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.BUILT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.noActionTransitionFunc,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.TRANSFERED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.pullApp,
		},
		common.TRANSFERING: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     sm.pullApp,
		},
		common.PUBLISHED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.PRESENT:     sm.noActionTransitionFunc,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.RUNNING: {
			common.RUNNING:     sm.noActionTransitionFunc,
			common.PRESENT:     sm.stopApp,
			common.BUILT:       sm.stopApp,
			common.PUBLISHED:   sm.removeAndPublishApp,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.DOWNLOADING: {
			common.PRESENT:     sm.pullApp,
			common.REMOVED:     sm.cancelPull,
			common.UNINSTALLED: sm.cancelPull,
		},
		common.STARTING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
		},
		common.STOPPING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
		},
		common.UPDATING: {
			common.PRESENT:     sm.cancelUpdate,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.DELETING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
	}

	return stateTransitionMap[prevState][nextState]
}

func (sm *StateMachine) setState(app *common.App, state common.AppState) error {
	err := sm.StateObserver.Notify(app, state)
	if err != nil {
		return err
	}
	return nil
}

func (sm *StateMachine) executeTransition(app *common.App, payload common.TransitionPayload, transitionFunc TransitionFunc) chan error {
	errChannel := make(chan error, 1)

	safe.Go(func() {
		log.Info().Msgf("Executing transition from %s to %s for %s (%s)...", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
		err := transitionFunc(payload, app)

		// send potential error to errChannel
		// if error = nil, the transition has completed successfully
		errChannel <- err

		// we are done sending, should close the channel
		close(errChannel)
	})

	return errChannel
}

func (sm *StateMachine) CancelTransition(app *common.App, payload common.TransitionPayload) chan error {
	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	transitionFunc := sm.getTransitionFunc(curAppState, payload.RequestedState)
	if transitionFunc == nil {
		log.Debug().Msgf("It appears the cancel transition function does not exist. In %s to %s for %s (%s)", curAppState, payload.RequestedState, payload.AppName, payload.Stage)
		return nil
	}

	return sm.executeTransition(app, payload, transitionFunc)
}

func (sm *StateMachine) InitTransition(app *common.App, payload common.TransitionPayload) chan error {
	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	var transitionFunc TransitionFunc
	if payload.RequestUpdate && payload.NewestVersion != app.Version {
		transitionFunc = sm.getUpdateTransition(payload, app)
	} else {
		transitionFunc = sm.getTransitionFunc(curAppState, payload.RequestedState)
	}

	if transitionFunc == nil {
		log.Debug().Msgf("Not yet implemented transition from %s to %s", curAppState, payload.RequestedState)
		return nil
	}

	return sm.executeTransition(app, payload, transitionFunc)
}
