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

func (clm *AppManager) retry(crashTask *CrashLoop) {
	crashTask.Retries++

	safe.Go(func() {
		var sleepTime time.Duration
		if crashTask.Retries == 0 {
			sleepTime = time.Second * 5
		} else {
			sleepTime = time.Second * 5 * time.Duration(crashTask.Retries)
		}

		// cap to 2,5 minutes
		sleepTime = time.Duration(common.Min(int64(sleepTime), int64(time.Millisecond*2500)))

		log.Info().Msgf("CrackLoopBackOff attempt: %d, sleeping for %s for %s (%s)", crashTask.Retries, sleepTime, crashTask.Payload.AppName, crashTask.Payload.Stage)

		time.Sleep(sleepTime)

		if crashTask.Retries == 30 {
			clm.crashLoopLock.Lock()
			delete(clm.crashLoops, crashTask)
			clm.crashLoopLock.Unlock()
		}

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

func (clm *AppManager) incrementCrashLoop(payload common.TransitionPayload) {
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

	if existingCrash != nil && existingCrash.Payload.RequestedState != payload.RequestedState {
		log.Debug().Msgf("requested state changed for %s (%s) to %s", payload.AppName, payload.Stage, payload.RequestedState)
		clm.clearCrashLoop(payload.AppKey, payload.Stage)
		existingCrash = nil
	}

	if existingCrash != nil {
		log.Debug().Msgf("retrying an existing crashloop for %s (%s)", payload.AppName, payload.Stage)
		clm.retry(existingCrash)
	} else {
		crashLoopTask := &CrashLoop{Payload: payload, Retries: 0}

		clm.crashLoopLock.Lock()
		clm.crashLoops[crashLoopTask] = struct{}{}
		clm.crashLoopLock.Unlock()

		log.Debug().Msgf("created a new crash loop for %s (%s)", payload.AppName, payload.Stage)
		clm.retry(crashLoopTask)
	}

}
