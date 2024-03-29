package benchmark

import (
	"fmt"
	"reagent/config"
	"reagent/safe"
	"time"
)

var PreConnectInit time.Time
var SocketConnectionInit time.Time
var SocketConnectionInitFromLaunch time.Time
var OnConnectInit time.Time
var OnConnectInitAfterConnection time.Time
var GreenInit time.Time

var TimeTillPreConnectInit time.Duration
var TimeTillSocketConnection time.Duration
var TimeTillOnConnect time.Duration
var TimeTillOnConnectAfterConnection time.Duration
var TimeTillSocketConnectionFromLaunch time.Duration
var TimeTillGreen time.Duration

func LogBenchmarkResultsWhenFinished(config *config.Config) {
	timers := []*time.Duration{&TimeTillPreConnectInit, &TimeTillSocketConnectionFromLaunch, &TimeTillSocketConnection, &TimeTillOnConnect, &TimeTillOnConnectAfterConnection, &TimeTillGreen}

	safe.Go(func() {
		for {
			finished := true
			for _, timer := range timers {
				if timer.Nanoseconds() == 0 {
					finished = false
				}
			}

			if finished {
				LogResults(config)
				break
			} else {
				time.Sleep(time.Millisecond * 500)
				continue
			}

		}
	})
}

func LogResults(config *config.Config) {
	initCompletionTimestamp := fmt.Sprintf("Time until pre connection initialization completion (from agent launch): %s", TimeTillPreConnectInit)
	connectionCompletionTimestamp := fmt.Sprintf("Time until socket connection established (from agent launch): %s", TimeTillSocketConnectionFromLaunch)
	connectionCompletionFromLaunchTimestamp := fmt.Sprintf("Time until socket connection established (from connection start): %s", TimeTillSocketConnection)
	onConnectCompletionTimestamp := fmt.Sprintf("Time until Onconnect handler finished (from agent launch): %s", TimeTillOnConnect)
	onConnectAfterSocketCompletionTimestamp := fmt.Sprintf("Time until Onconnect handler finished (from socket connection): %s", TimeTillOnConnectAfterConnection)
	greenTimestamp := fmt.Sprintf("Time until 'green': %s", TimeTillGreen)

	config.StartupLogChannel <- "Benchmarks:"
	config.StartupLogChannel <- "----------------------------------"
	config.StartupLogChannel <- initCompletionTimestamp
	config.StartupLogChannel <- connectionCompletionTimestamp
	config.StartupLogChannel <- connectionCompletionFromLaunchTimestamp
	config.StartupLogChannel <- onConnectCompletionTimestamp
	config.StartupLogChannel <- onConnectAfterSocketCompletionTimestamp
	config.StartupLogChannel <- greenTimestamp
	config.StartupLogChannel <- "----------------------------------"

	close(config.StartupLogChannel)
}
