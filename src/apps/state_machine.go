package apps

import (
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/logging"

	"github.com/rs/zerolog/log"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver *StateObserver
	Container     container.Container
	LogManager    *logging.LogManager
	appStates     []*common.App
}

func NewStateMachine(container container.Container, logManager *logging.LogManager, observer *StateObserver) StateMachine {
	appStates := make([]*common.App, 0)
	return StateMachine{
		StateObserver: observer,
		Container:     container,
		LogManager:    logManager,
		appStates:     appStates,
	}
}

func (sm *StateMachine) getTransitionFunc(prevState common.AppState, nextState common.AppState) TransitionFunc {
	var stateTransitionMap = map[common.AppState]map[common.AppState]TransitionFunc{
		common.REMOVED: {
			common.PRESENT:     sm.pullApp,
			common.RUNNING:     sm.pullAndRunApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.UNINSTALLED: {
			common.PRESENT:   sm.pullApp,
			common.RUNNING:   sm.runApp,
			common.BUILT:     sm.buildApp,
			common.PUBLISHED: sm.publishApp,
		},
		common.BUILDING: {
			common.REMOVED: sm.cancelBuild,
		},
		common.PRESENT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     sm.runApp,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.FAILED: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: nil,
			common.PRESENT:     sm.recoverFailToPresentHandler,
			common.RUNNING:     sm.recoverFailToRunningHandler,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.BUILT: {
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
			common.PRESENT:     nil,
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
			common.PRESENT:     nil,
			common.BUILT:       sm.buildApp,
			common.PUBLISHED:   sm.publishApp,
		},
		common.RUNNING: {
			common.PRESENT:     sm.stopApp,
			common.BUILT:       sm.stopApp,
			common.PUBLISHED:   sm.removeAndPublishApp,
			common.REMOVED:     sm.removeApp,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.DOWNLOADING: {
			common.PRESENT:     nil,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
		},
		common.STARTING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.STOPPING: {
			common.PRESENT:     sm.stopApp,
			common.REMOVED:     nil,
			common.UNINSTALLED: sm.uninstallApp,
			common.RUNNING:     nil,
		},
		common.UPDATING: {
			common.PRESENT:     nil,
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
	errChannel := make(chan error)

	go func() {
		log.Info().Msgf("Executing transition from %s to %s for %s (%s)...", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
		err := transitionFunc(payload, app)

		// If anything goes wrong with the transition function
		// we should set the state change to FAILED
		// This will in turn update the in memory state and the local database state
		// which will in turn update the remote database as well
		if err != nil {
			setStateErr := sm.setState(app, common.FAILED)
			if setStateErr != nil {
				// wrap errors into one
				err = fmt.Errorf("Failed to complete transition: %w; Failed to set state to 'FAILED';", err)
			}
		}

		// send potential error to errChannel
		// if error = nil, the transition has completed successfully
		errChannel <- err

		// we are done sending, should close the channel
		close(errChannel)
	}()

	return errChannel
}

func (sm *StateMachine) CancelTransition(app *common.App, payload common.TransitionPayload) chan error {
	transitionFunc := sm.getTransitionFunc(app.CurrentState, payload.RequestedState)
	return sm.executeTransition(app, payload, transitionFunc)
}

func (sm *StateMachine) PerformTransition(app *common.App, payload common.TransitionPayload) chan error {
	var transitionFunc TransitionFunc
	if payload.RequestUpdate && payload.NewestVersion != app.Version {
		transitionFunc = sm.getUpdateTransition(payload, app)
	} else {
		transitionFunc = sm.getTransitionFunc(app.CurrentState, payload.RequestedState)
	}

	if transitionFunc == nil {
		log.Debug().Msgf("Not yet implemented transition from %s to %s", app.CurrentState, payload.RequestedState)
		return nil
	}

	return sm.executeTransition(app, payload, transitionFunc)
}
