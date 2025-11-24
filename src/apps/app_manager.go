package apps

import (
	"fmt"
	"os"
	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger/topics"
	"reagent/safe"
	"reagent/store"
	"reagent/tunnel"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// Set zerolog to use a pretty console writer for human-friendly logs
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"})
}

type AppManager struct {
	AppStore      *store.AppStore
	StateMachine  *StateMachine
	StateObserver *StateObserver
	tunnelManager tunnel.TunnelManager
	crashLoops    map[*CrashLoop]struct{}
	crashLoopLock sync.Mutex
}

func NewAppManager(sm *StateMachine, as *store.AppStore, so *StateObserver, tm tunnel.TunnelManager) *AppManager {
	am := AppManager{
		StateMachine:  sm,
		StateObserver: so,
		AppStore:      as,
		tunnelManager: tm,
		crashLoops:    make(map[*CrashLoop]struct{}),
	}

	am.StateObserver.AppManager = &am
	return &am
}

func (am *AppManager) syncPortState(payload common.TransitionPayload, app *common.App) error {
	log.Debug().Str("app", payload.AppName).Msg("syncPortState called")
	globalConfig := am.StateMachine.Container.GetConfig()

	if payload.Stage == common.DEV {
		log.Debug().Msg("Skipping syncPortState for DEV stage")
		return nil
	}

	if runtime.GOOS == "windows" {
		log.Warn().Msg("Tunneling feature is not supported on Windows")
		return nil
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	requestedState := app.RequestedState
	app.StateLock.Unlock()

	portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to convert interface to port forward rule")
		return err
	}

	newPorts := make([]common.PortForwardRule, 0)

	for _, portRule := range portRules {
		subdomain := tunnel.CreateSubdomain(tunnel.Protocol(portRule.Protocol), uint64(globalConfig.ReswarmConfig.DeviceKey), payload.AppName, portRule.Port)
		tunnelID := tunnel.CreateTunnelID(subdomain, portRule.Protocol)
		newConfig := tunnel.TunnelConfig{}
		if portRule.Active {
			if requestedState == common.RUNNING || curAppState == common.RUNNING {
				tnl := am.tunnelManager.Get(tunnelID)
				if tnl != nil {
					log.Debug().Str("tunnelID", tunnelID).Msg("Tunnel already exists, skipping add")
				} else {

					tunnelConfig := tunnel.TunnelConfig{
						Subdomain:  subdomain,
						AppName:    payload.AppName,
						Protocol:   tunnel.Protocol(portRule.Protocol),
						LocalPort:  portRule.Port,
						LocalIP:    portRule.LocalIP,
						RemotePort: portRule.RemotePort,
					}

					newConfig, err = am.tunnelManager.AddTunnel(tunnelConfig)
					if err != nil {
						log.Error().Stack().Err(err).Msg("Failed to add tunnel")
					}
				}
				// continue
			}
		} else {
			// Remove tunnel when it's not active and app is not running
			tnl := am.tunnelManager.Get(tunnelID)
			if tnl != nil {
				log.Info().Str("tunnelID", tunnelID).Msg("Removing tunnel as it is not active and app is not running")
				err := am.tunnelManager.RemoveTunnel(tnl.Config)
				if err != nil {
					log.Error().Stack().Err(err).Msg("Failed to remove tunnel")
				}
			}
		}

		newPort := portRule
		if newConfig.RemotePort != 0 {
			newPort.RemotePort = newConfig.RemotePort
			log.Info().Str("app", payload.AppName).Int("localPort", int(portRule.Port)).Int("remotePort", int(newPort.RemotePort)).Msg("Assigned new remote port for tunnel")
		}
		newPorts = append(newPorts, newPort)

	}

	np, err := tunnel.PortForwardRuleToInterface(newPorts)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to convert newPorts to interface")
		return err
	}

	payload.Ports = np
	am.tunnelManager.SaveRemotePorts(payload)

	// err = am.AppStore.UpdateLocalRequestedState(payload)
	// if err != nil {
	// 	return err
	// }

	am.UpdateTunnelState()

	return nil
}

func (am *AppManager) UpdateTunnelState() error {
	updateTopic := common.BuildTunnelStateUpdate(am.StateMachine.Container.GetConfig().ReswarmConfig.SerialNumber)
	tunnelStates, err := am.tunnelManager.GetState()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get tunnel states")
		return err
	}

	var args []interface{}
	for _, tunnelState := range tunnelStates {
		args = append(args, tunnelState)
	}

	log.Debug().Msg("Publishing tunnel state update")
	return am.AppStore.Messenger.Publish(topics.Topic(updateTopic), args, nil, nil)
}

func (am *AppManager) RequestAppState(payload common.TransitionPayload) error {
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get app in RequestAppState")
		return err
	}

	app.StateLock.Lock()
	curAppState := app.CurrentState
	requestedAppState := app.RequestedState
	app.StateLock.Unlock()

	log.Debug().Str("app", payload.AppName).Str("from", string(curAppState)).Str("to", string(requestedAppState)).Msg("Received Requested State")

	// clear crashloop counter if changing state request
	if !payload.Retrying {
		am.clearCrashLoop(app.AppKey, app.Stage)
	}

	// TODO: get rid of this ugly patch: cancel any filetransfers for this container on stop press
	if (curAppState == common.UNINSTALLED || curAppState == common.REMOVED || curAppState == common.PRESENT || curAppState == common.FAILED) &&
		(requestedAppState == common.PRESENT || requestedAppState == common.BUILT) && payload.Stage == common.DEV {
		log.Debug().Msg("Canceling file transfer due to state change")
		am.StateMachine.Filesystem.CancelFileTransfer(payload.ContainerName.Dev)
	}

	if payload.CancelTransition {
		log.Debug().Msgf("Cancel request was received for %s (%s) (currently: %s)", app.AppName, app.Stage, app.CurrentState)
		am.StateMachine.CancelTransition(app, payload)
		return nil
	}

	err = am.syncPortState(payload, app)
	if err != nil {
		log.Error().Stack().Err(err).Msgf("failed to sync port state")
		return err
	}

	locked := app.SecureTransition() // if the app is not locked, it will lock the app
	if locked {
		log.Info().Msgf("App with name %s and stage %s is already transitioning. (CURRENT STATE: %s)", app.AppName, app.Stage, app.CurrentState)
		return nil
	}

	// need to call this after we have secured the lock
	// to not change the actual state in the middle of an ongoing transition
	// this is necessary because some state transitions require a change of actual state (BUILD & PUBLISH)
	err = am.UpdateCurrentAppState(payload)
	if err != nil {
		app.UnlockTransition()
		log.Error().Stack().Err(err).Msg("Failed to update current app state")
		return err
	}

	// before we transition, should request the token
	token, err := am.AppStore.GetRegistryToken(payload.RequestorAccountKey)
	if err != nil {
		app.UnlockTransition()
		log.Error().Stack().Err(err).Msg("Failed to get registry token")
		return err
	}

	payload.RegisteryToken = token

	errC := am.StateMachine.InitTransition(app, payload)
	if errC == nil {
		// not yet implemented or nullified state transition
		app.UnlockTransition()
		log.Debug().Msg("InitTransition returned nil channel, nothing to do")
		return nil
	}

	// block till transition has finished
	select {
	case err := <-errC:
		app.UnlockTransition()

		if errdefs.IsNoActionTransition(err) {
			log.Debug().Msg("A no action transition was executed, nothing to do. Will also not verify")

			// Ensure the remote state == current state
			err := am.StateObserver.Notify(app, app.CurrentState)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("failed to ensure remote state")
				return err
			}

			am.UpdateTunnelState()

			return nil
		}

		isCanceled := errdefs.IsDockerStreamCanceled(err)
		if err == nil || isCanceled {
			if !isCanceled {
				log.Info().Msgf("Successfully finished transaction for App (%s, %s)", app.AppName, app.Stage)
			} else {
				log.Info().Msgf("Successfully canceled transition for App (%s, %s)", app.AppName, app.Stage)
			}

			// Verify if app has the latest requested state
			// TODO: properly handle it when verifying fails
			err := am.VerifyState(app)
			if err != nil {
				log.Error().Stack().Err(err).Msgf("failed to verify app state")
				return err
			}

			am.UpdateTunnelState()

			return nil
		}

		// If anything goes wrong with the transition function
		// we should set the state change to FAILED
		// This will in turn update the in memory state and the local database state
		// which will in turn update the remote database as well
		if err != nil {
			setStateErr := am.StateObserver.Notify(app, common.FAILED)
			if setStateErr != nil {
				// wrap errors into one
				err = fmt.Errorf("failed to complete transition: %w", err)
			}
		}

		log.Error().Msgf("An error occured during transition from %s to %s for %s (%s)", app.CurrentState, payload.RequestedState, app.AppName, app.Stage)
		log.Error().Stack().Err(err).Msgf("The app state for %s (%s) has been set to FAILED", app.AppName, app.Stage)

		// enter the crashloop when we encounter a FAILED state
		if payload.Stage == common.PROD {
			am.incrementCrashLoop(payload)
		}
	}

	return nil
}

func IsInvalidOfflineTransition(app *common.App, payload common.TransitionPayload) bool {
	app.StateLock.Lock()
	defer app.StateLock.Unlock()

	notInstalled := app.CurrentState == common.REMOVED || app.CurrentState == common.UNINSTALLED
	buildRequest := app.RequestedState == common.BUILT
	removalRequest := payload.RequestedState == common.REMOVED || payload.RequestedState == common.UNINSTALLED

	if buildRequest {
		return true
	}

	// if the app is not on the device and we do any transition that would require internet we return true
	if notInstalled && payload.Stage == common.PROD && !removalRequest {
		return true
	}

	// cannot publish, update apps while offline
	if payload.RequestedState == common.PUBLISHED || (payload.RequestedState == common.PRESENT && payload.RequestUpdate) {
		return true
	}

	return false
}

func (am *AppManager) EnsureLocalRequestedStates() error {
	log.Debug().Msg("Ensuring local requested states")
	rStates, err := am.AppStore.GetRequestedStates()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get requested states")
		return err
	}

	// Stagger the startup to avoid overwhelming the backend
	for idx := range rStates {
		payload := rStates[idx]

		app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
		if err != nil {
			log.Error().Stack().Err(err).Msg("Failed to get app in EnsureLocalRequestedStates")
			return err
		}

		safe.Go(func() {
			app.StateLock.Lock()
			currentAppState := app.CurrentState
			app.StateLock.Unlock()

			if !IsInvalidOfflineTransition(app, payload) && currentAppState != payload.RequestedState {
				err := am.RequestAppState(payload)
				if err != nil {
					log.Error().Stack().Err(err).Msg("Failed to ensure local requested state")
				}
			}
		})

		// Add a small delay between starting each app to avoid overwhelming the backend
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

func (am *AppManager) VerifyState(app *common.App) error {
	log.Debug().Str("app", app.AppName).Msg("Verifying app state")
	log.Printf("Verifying if app (%s, %s) is in latest state...", app.AppName, app.Stage)

	requestedStatePayload, err := am.AppStore.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get requested state in VerifyState")
		return err
	}

	log.Info().Msgf("Latest requested state (verify): %s", requestedStatePayload.RequestedState)

	app.StateLock.Lock()
	curAppState := app.CurrentState
	requestedState := app.RequestedState
	app.StateLock.Unlock()

	if curAppState == common.FAILED {
		log.Debug().Msg("App transition finished in a failed state")
		return nil
	}

	if curAppState != requestedState {
		log.Debug().Msgf("App (%s, %s) is not in latest state (%s), transitioning to %s...", app.AppName, app.Stage, curAppState, requestedState)

		// transition again
		safe.Go(func() {
			_ = am.RequestAppState(requestedStatePayload)
		})
	} else {
		err = am.syncPortState(requestedStatePayload, app)
		if err != nil {
			log.Error().Stack().Err(err).Msgf("failed to sync port state")
			return err
		}
	}

	if curAppState == common.BUILT && requestedState == common.BUILT {
		// The build has finished and should now be put to PRESENT
		err = am.StateObserver.Notify(app, common.PRESENT)
		if err != nil {
			log.Error().Stack().Err(err).Msg("Failed to notify state observer for PRESENT state")
			return err
		}
	}

	return nil
}

func (am *AppManager) UpdateCurrentAppState(payload common.TransitionPayload) error {
	log.Debug().Str("app", payload.AppName).Msg("UpdateCurrentAppState called")
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get app in UpdateCurrentAppState")
		return err
	}

	app.StateLock.Lock()

	curAppState := app.CurrentState

	if curAppState == common.BUILT ||
		curAppState == common.PUBLISHED ||
		(curAppState == common.PRESENT && app.RequestedState == common.BUILT) {
		if payload.CurrentState != "" {
			app.CurrentState = payload.CurrentState
		}
	}

	if payload.PresentVersion != "" {
		app.Version = payload.PresentVersion
	}

	app.StateLock.Unlock()

	log.Debug().Str("app", payload.AppName).Msg("Updating local app state")
	return am.AppStore.UpdateLocalAppState(app, curAppState)
}

func (am *AppManager) CreateOrUpdateApp(payload common.TransitionPayload) error {
	log.Debug().Str("app", payload.AppName).Msg("CreateOrUpdateApp called")
	app, err := am.AppStore.GetApp(payload.AppKey, payload.Stage)
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get app in CreateOrUpdateApp")
		return err
	}

	payload.RequestedState = common.TransientToActualState(payload.RequestedState)

	if app == nil {
		app, err = am.AppStore.AddApp(payload)
		if err != nil {
			log.Error().Stack().Err(err).Msg("Failed to add app in CreateOrUpdateApp")
			return err
		}
	}

	app.StateLock.Lock()

	if app.CurrentState == common.BUILT && app.RequestedState != common.BUILT {
		app.StateLock.Unlock()
		log.Debug().Str("app", payload.AppName).Msg("Skipping update of requestedState as app is already built")
		return nil
	}

	app.RequestedState = payload.RequestedState

	app.StateLock.Unlock()

	log.Debug().Str("app", payload.AppName).Msg("Updating local requested app state")
	return am.AppStore.UpdateLocalRequestedState(payload)
}

// EnsureRemoteRequestedStates iterates over all requested states found in the local database, and transitions were neccessary.
func (am *AppManager) EnsureRemoteRequestedStates() error {
	log.Debug().Msg("Ensuring remote requested states")
	payloads, err := am.AppStore.GetRequestedStates()
	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get requested states in EnsureRemoteRequestedStates")
		return err
	}

	for i := range payloads {
		payload := payloads[i]

		// do not execute publishes on reconnect
		if payload.Stage == common.DEV || payload.RequestedState == common.PUBLISHED || payload.RequestedState == common.BUILT {
			log.Debug().Str("app", payload.AppName).Msg("Skipping publish on reconnect for DEV/PUBLISHED/BUILT state")
			continue
		}

		safe.Go(func() {
			log.Debug().Str("app", payload.AppName).Msg("Requesting app state in goroutine")
			am.RequestAppState(payload)
		})
	}

	return nil
}

// UpdateLocalRequestedAppStatesWithRemote is responsible for fetching any requested app states from the remote database.
// The local database will be updated with the fetched requested states. In case an app state does exist yet locally, one will be created.
func (am *AppManager) UpdateLocalRequestedAppStatesWithRemote() error {
	log.Debug().Msg("Updating local requested app states with remote")
	newestPayloads, err := am.AppStore.FetchRequestedAppStates()
	if err != nil {
		return err
	}

	log.Info().Msgf("Found %d app states, updating local database with new requested states..", len(newestPayloads))
	for i := range newestPayloads {
		payload := newestPayloads[i]

		// portRules, err := tunnel.InterfaceToPortForwardRule(payload.Ports)
		// if err != nil {
		// 	return err
		// }

		if payload.Stage == common.PROD {
			_, err := am.AppStore.GetApp(payload.AppKey, common.PROD)
			if err != nil {
				return err
			}

			err = am.CreateOrUpdateApp(payload)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
