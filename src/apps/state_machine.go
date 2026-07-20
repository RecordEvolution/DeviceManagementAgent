package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/diskguard"
	"reagent/errdefs"
	"reagent/filesystem"
	"reagent/logging"
	"reagent/safe"
	"sync"

	"github.com/rs/zerolog/log"
)

type TransitionFunc func(TransitionPayload common.TransitionPayload, app *common.App) error

type StateMachine struct {
	StateObserver *StateObserver
	Filesystem    *filesystem.Filesystem
	Container     container.Container
	LogManager    *logging.LogManager
	appStates     []*common.App

	// composeUpdateCancels holds the cancel func of each in-flight compose
	// update, keyed by app. A PRESENT request during an update calls it (via
	// cancelUpdate) to kill the `docker compose` CLI so a hung pull/down unwinds
	// and releases the transition lock, instead of blocking on cmd.Wait() forever.
	composeUpdateCancels     map[string]context.CancelFunc
	composeUpdateCancelMutex sync.Mutex
}

func NewStateMachine(container container.Container, logManager *logging.LogManager, observer *StateObserver, filesystem *filesystem.Filesystem) StateMachine {
	appStates := make([]*common.App, 0)
	return StateMachine{
		StateObserver:        observer,
		Container:            container,
		LogManager:           logManager,
		Filesystem:           filesystem,
		appStates:            appStates,
		composeUpdateCancels: make(map[string]context.CancelFunc),
	}
}

func composeUpdateKey(stage common.Stage, appKey uint64) string {
	return fmt.Sprintf("%s_%d", stage, appKey)
}

// registerComposeUpdateCancel records the cancel func of an in-flight compose
// update so cancelUpdate can reach it.
func (sm *StateMachine) registerComposeUpdateCancel(stage common.Stage, appKey uint64, cancel context.CancelFunc) {
	key := composeUpdateKey(stage, appKey)
	sm.composeUpdateCancelMutex.Lock()
	sm.composeUpdateCancels[key] = cancel
	sm.composeUpdateCancelMutex.Unlock()
}

// clearComposeUpdateCancel drops the cancel func once the update has unwound.
func (sm *StateMachine) clearComposeUpdateCancel(stage common.Stage, appKey uint64) {
	key := composeUpdateKey(stage, appKey)
	sm.composeUpdateCancelMutex.Lock()
	delete(sm.composeUpdateCancels, key)
	sm.composeUpdateCancelMutex.Unlock()
}

// cancelComposeUpdate cancels the context of an in-flight compose update for the
// app, killing the docker compose CLI. Returns true if one was in flight.
func (sm *StateMachine) cancelComposeUpdate(stage common.Stage, appKey uint64) bool {
	key := composeUpdateKey(stage, appKey)
	sm.composeUpdateCancelMutex.Lock()
	cancel := sm.composeUpdateCancels[key]
	sm.composeUpdateCancelMutex.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
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
			common.REMOVED:     sm.cancelTransfer,
			common.UNINSTALLED: sm.cancelTransfer,
			common.PRESENT:     sm.cancelTransfer,
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
			common.REMOVED:     sm.cancelUpdateAndRemove,
			common.UNINSTALLED: sm.cancelUpdateAndUninstall,
			common.RUNNING:     nil,
		},
		common.DELETING: {
			common.PRESENT:     nil,
			common.REMOVED:     sm.removeApp,
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
	// While the device is in a disk-emergency, refuse transitions that would
	// pull, build, or start an app and consume more disk. Fail fast: the caller
	// marks the app FAILED and reports it to the cloud.
	if diskguard.IsEmergency() && common.IsDiskGrowingState(payload.RequestedState) {
		log.Warn().Msgf("disk-emergency: refusing transition to %s for %s (%s)", payload.RequestedState, payload.AppName, payload.Stage)
		errChannel := make(chan error, 1)
		errChannel <- fmt.Errorf("disk emergency: device is critically low on storage; refusing to transition %s (%s) to %s", payload.AppName, payload.Stage, payload.RequestedState)
		close(errChannel)
		return errChannel
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	var transitionFunc TransitionFunc
	if payload.RequestUpdate && payload.NewestVersion != app.Version && payload.RequestedState != common.UNINSTALLED {
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

func (sm *StateMachine) HandleRegistryLoginsWithDefault(payload common.TransitionPayload) error {
	config := sm.Container.GetConfig()

	if payload.DockerCredentials == nil {
		payload.DockerCredentials = make(map[string]common.DockerCredential)
	}

	payload.DockerCredentials[config.ReswarmConfig.DockerRegistryURL] = common.DockerCredential{
		Username: payload.RegisteryToken,
		Password: config.ReswarmConfig.Secret,
	}

	return sm.Container.HandleRegistryLogins(payload.DockerCredentials)
}
