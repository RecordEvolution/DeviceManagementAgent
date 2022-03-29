package apps

import (
	"reagent/common"
	"reagent/safe"
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

func calculateLoopSleepTime(retries uint) time.Duration {
	var sleepTime time.Duration
	if retries == 0 {
		sleepTime = time.Second * 5
	} else {
		sleepTime = time.Second * 5 * time.Duration(retries)
	}

	// cap to 2,5 minutes
	sleepTime = time.Duration(common.Min(int64(sleepTime), int64(time.Second*150)))

	return sleepTime
}

func (clm *AppManager) retry(crashTask *CrashLoop) {
	crashTask.Retries++

	safe.Go(func() {

		// cap to 2,5 minutes
		sleepTime := calculateLoopSleepTime(crashTask.Retries)

		log.Info().Msgf("CrashLoopBackOff attempt: %d, sleeping for %s for %s (%s)", crashTask.Retries, sleepTime, crashTask.Payload.AppName, crashTask.Payload.Stage)

		time.Sleep(sleepTime)

		if crashTask.Retries == 30 {
			clm.crashLoopLock.Lock()
			crashTask.Retries = 0
			clm.crashLoopLock.Unlock()
		}

		// exit the goroutine if the crashloop was canceled in the meantime
		clm.crashLoopLock.Lock()
		var foundTask *CrashLoop
		for task := range clm.crashLoops {
			if task.Payload.Stage == crashTask.Payload.Stage && task.Payload.AppKey == crashTask.Payload.AppKey {
				foundTask = task
				break
			}
		}

		if foundTask == nil {
			log.Debug().Msgf("Crashloop task no longer exists for %d (%s), exiting goroutine...", crashTask.Payload.AppKey, crashTask.Payload.Stage)
			clm.crashLoopLock.Unlock()
			return
		}

		clm.crashLoopLock.Unlock()

		app, _ := clm.AppStore.GetApp(crashTask.Payload.AppKey, crashTask.Payload.Stage)
		if app.CurrentState != crashTask.Payload.RequestedState {
			clm.RequestAppState(crashTask.Payload)
		} else {
			clm.clearCrashLoop(crashTask.Payload.AppKey, crashTask.Payload.Stage)
		}

	})
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
		clm.retry(existingCrash)

		sleepTime := calculateLoopSleepTime(existingCrash.Retries)
		return existingCrash.Retries, sleepTime
	} else {
		payload.Retrying = true
		crashLoopTask := &CrashLoop{Payload: payload, Retries: 0}

		clm.crashLoopLock.Lock()
		clm.crashLoops[crashLoopTask] = struct{}{}
		clm.crashLoopLock.Unlock()

		log.Debug().Msgf("created a new crash loop for %s (%s)", payload.AppName, payload.Stage)
		clm.retry(crashLoopTask)

		sleepTime := calculateLoopSleepTime(crashLoopTask.Retries)
		return crashLoopTask.Retries, sleepTime
	}
}
