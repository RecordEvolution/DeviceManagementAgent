package apps

import (
	"math/rand"
	"reagent/common"
	"reagent/safe"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type CrashLoopManager struct {
	AppManager *AppManager
}

type CrashLoop struct {
	Payload common.TransitionPayload
	Retries uint
}

var (
	jitterRand     = rand.New(rand.NewSource(time.Now().UnixNano()))
	jitterRandLock sync.Mutex
)

// calculateLoopSleepTime returns the backoff duration before the next restart
// attempt. The curve is quadratic (5s * retries^2) capped at 1 hour, with ±20%
// jitter to avoid synchronized retries across a fleet. The polynomial shape is
// deliberately gentler than exponential so that apps waiting on slow
// dependencies (peer machines coming online, network ready, mounts appearing)
// get many patient retries before the interval stretches to the cap.
func calculateLoopSleepTime(retries uint) time.Duration {
	if retries == 0 {
		retries = 1
	}
	// Guard against overflow; the cap kicks in well before this anyway.
	if retries > 100 {
		retries = 100
	}

	sleepTime := time.Second * 5 * time.Duration(retries) * time.Duration(retries)
	if sleepTime > time.Hour {
		sleepTime = time.Hour
	}

	jitterRandLock.Lock()
	jitter := 0.8 + jitterRand.Float64()*0.4 // factor in [0.8, 1.2)
	jitterRandLock.Unlock()

	return time.Duration(float64(sleepTime) * jitter)
}

func (clm *AppManager) retry(crashTask *CrashLoop) uint {
	// Mutate and snapshot Retries under the lock; the spawned goroutine reads
	// only the snapshot. Without this, the next retry() call's Retries++ races
	// with this goroutine's read (crashTask is a shared pointer reused across
	// retries). Payload is immutable after task creation, so it is safe to read.
	clm.crashLoopLock.Lock()
	crashTask.Retries++
	retries := crashTask.Retries
	clm.crashLoopLock.Unlock()

	safe.Go(func() {
		sleepTime := calculateLoopSleepTime(retries)

		log.Info().Msgf("CrashLoopBackOff attempt: %d, sleeping for %s for %s (%s)", retries, sleepTime, crashTask.Payload.AppName, crashTask.Payload.Stage)

		time.Sleep(sleepTime)

		clm.crashLoopWake(crashTask)
	})

	return retries
}

// crashLoopWake is the decision a crashloop goroutine makes after its backoff
// sleep. Split from retry() so it is testable without sleeping.
func (clm *AppManager) crashLoopWake(crashTask *CrashLoop) {
	// Exit if THIS loop was canceled in the meantime. Looked up by task
	// identity, not by app key: a loop cleared and re-created for the same app
	// while this goroutine slept is a different loop with its own goroutine, so
	// a key match would let this stale goroutine double-drive retries and
	// inflate the new loop's backoff counter.
	clm.crashLoopLock.Lock()
	_, active := clm.crashLoops[crashTask]
	clm.crashLoopLock.Unlock()

	if !active {
		log.Debug().Msgf("Crashloop task no longer exists for %d (%s), exiting goroutine...", crashTask.Payload.AppKey, crashTask.Payload.Stage)
		return
	}

	app, err := clm.AppStore.GetApp(crashTask.Payload.AppKey, crashTask.Payload.Stage)
	if err != nil || app == nil {
		return
	}

	app.StateLock.Lock()
	currentState := app.CurrentState
	memRequestedState := app.RequestedState
	app.StateLock.Unlock()

	// Re-read the authoritative requested-state row. The captured payload may
	// be doubly stale: the TARGET may have changed (an uninstall arrived
	// mid-backoff — re-driving the old target forever kept a
	// registry-crashlooping app re-trying RUNNING for hours after its
	// uninstall was requested), and even for an unchanged target the CONFIG
	// (environment, compose definition, ports) may have been updated.
	rowPayload, rowErr := clm.AppStore.GetRequestedState(crashTask.Payload.AppKey, crashTask.Payload.Stage)

	if rowErr != nil {
		if memRequestedState != crashTask.Payload.RequestedState {
			// The target changed but its row is gone or unreadable — most
			// likely a completed teardown deleted it. Never resurrect the
			// captured target: just end this loop.
			log.Info().Msgf("crashloop for %s (%s): requested state changed and its row is gone; ending the loop",
				crashTask.Payload.AppName, crashTask.Payload.Stage)
			clm.clearCrashLoop(crashTask.Payload.AppKey, crashTask.Payload.Stage)
			return
		}

		// The row is unreadable here, so the pending-update check (see below)
		// must use the CAPTURED payload — it carries the update's versions.
		if currentState == crashTask.Payload.RequestedState && !hasPendingUpdate(crashTask.Payload) {
			clm.clearCrashLoop(crashTask.Payload.AppKey, crashTask.Payload.Stage)
			return
		}

		log.Warn().Err(rowErr).Msgf("crashloop for %s (%s): failed to refresh the requested-state payload; retrying the captured one",
			crashTask.Payload.AppName, crashTask.Payload.Stage)
		retryPayload := crashTask.Payload
		retryPayload.Retrying = true
		clm.RequestAppState(retryPayload)
		return
	}

	// Decide "did the target change?" against the ROW, not the in-memory
	// value: the two diverge when a row write failed (e.g. disk-full), and
	// treating that divergence as new intent on every wake would clear and
	// re-create the loop each cycle, resetting the backoff to seconds forever.
	if rowPayload.RequestedState != crashTask.Payload.RequestedState {
		// New intent: end this loop and drive the app toward the current
		// target. Not marked Retrying, so a failure starts its own crashloop
		// from a fast backoff.
		log.Info().Msgf("crashloop for %s (%s): requested state changed from %s to %s during backoff; dropping the stale retry",
			crashTask.Payload.AppName, crashTask.Payload.Stage, crashTask.Payload.RequestedState, rowPayload.RequestedState)
		clm.clearCrashLoop(crashTask.Payload.AppKey, crashTask.Payload.Stage)

		if currentState == rowPayload.RequestedState && !hasPendingUpdate(rowPayload) {
			return
		}

		carryPushOnlyFields(&rowPayload, crashTask.Payload)
		clm.RequestAppState(rowPayload)
		return
	}

	// Being AT the target state only ends the loop when nothing is pending.
	// Since updates pull the new release BEFORE tearing the old one down, a
	// failed update leaves the old version running and the observer corrects
	// the FAILED blip back to RUNNING — the very state the update was requested
	// from. Treating that as "converged" would silently drop the pending update
	// forever; the row's request_update flag says whether the work is done.
	if currentState == crashTask.Payload.RequestedState && !hasPendingUpdate(rowPayload) {
		clm.clearCrashLoop(crashTask.Payload.AppKey, crashTask.Payload.Stage)
		return
	}

	// Same target: retry with the refreshed row payload so config updates are
	// honored. Retrying stays set so RequestAppState does not clear this loop
	// and the backoff keeps growing.
	carryPushOnlyFields(&rowPayload, crashTask.Payload)
	rowPayload.Retrying = true
	clm.RequestAppState(rowPayload)
}

// carryPushOnlyFields copies the payload fields that exist only on live cloud
// pushes — the requested-state row does not persist them — from the captured
// crashloop payload onto a row-built one. Without this a retry would lose the
// app's third-party registry credentials (docker_credentials), its instance
// identity (INSTANCE_KEY), and its credential epoch, degrading every restart
// the crashloop drives.
func carryPushOnlyFields(rowPayload *common.TransitionPayload, captured common.TransitionPayload) {
	rowPayload.DockerCredentials = captured.DockerCredentials
	rowPayload.InstanceKey = captured.InstanceKey
	rowPayload.AppCredEpoch = captured.AppCredEpoch
	rowPayload.DeviceToAppKey = captured.DeviceToAppKey
}

func (clm *AppManager) clearCrashLoop(appKey uint64, stage common.Stage) {
	clm.crashLoopLock.Lock()
	var foundTask *CrashLoop
	for crashTask := range clm.crashLoops {
		if crashTask.Payload.Stage == stage && crashTask.Payload.AppKey == appKey {
			foundTask = crashTask
			break
		}
	}

	if foundTask != nil {
		log.Debug().Msgf("clearing an existing crashloop for %d (%s)", appKey, stage)
		delete(clm.crashLoops, foundTask)
	}

	clm.crashLoopLock.Unlock()
}

func (clm *AppManager) incrementCrashLoop(payload common.TransitionPayload) (uint, time.Duration) {
	clm.crashLoopLock.Lock()
	existingCrashes := clm.crashLoops

	var existingCrash *CrashLoop
	for crash := range existingCrashes {
		if crash.Payload.Stage == payload.Stage &&
			crash.Payload.AppKey == payload.AppKey {
			existingCrash = crash
			break
		}
	}
	clm.crashLoopLock.Unlock()

	if existingCrash != nil {
		log.Debug().Msgf("retrying an existing crashloop for %s (%s)", payload.AppName, payload.Stage)
		retries := clm.retry(existingCrash)

		sleepTime := calculateLoopSleepTime(retries)
		return retries, sleepTime
	} else {
		payload.Retrying = true
		crashLoopTask := &CrashLoop{Payload: payload, Retries: 0}

		clm.crashLoopLock.Lock()
		clm.crashLoops[crashLoopTask] = struct{}{}
		clm.crashLoopLock.Unlock()

		log.Debug().Msgf("created a new crash loop for %s (%s)", payload.AppName, payload.Stage)
		retries := clm.retry(crashLoopTask)

		sleepTime := calculateLoopSleepTime(retries)
		return retries, sleepTime
	}
}
