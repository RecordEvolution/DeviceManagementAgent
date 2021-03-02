package benchmark

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

var AgentInit time.Time
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
var TimeTillAgentInit time.Duration
var TimeTillGreen time.Duration

func LogResults() {
	initCompletionTimestamp := fmt.Sprintf("Time until pre connection initialization completion (From agent launch): %s", TimeTillPreConnectInit)
	connectionCompletionTimestamp := fmt.Sprintf("Time until socket connection established (from agent launch): %s", TimeTillSocketConnectionFromLaunch)
	connectionCompletionFromLaunchTimestamp := fmt.Sprintf("Time until socket connection established (from connection start): %s", TimeTillSocketConnection)
	onConnectCompletionTimestamp := fmt.Sprintf("Time until Onconnect handler finished (from agent launch): %s", TimeTillOnConnect)
	onConnectAfterSocketCompletionTimestamp := fmt.Sprintf("Time until Onconnect handler finished (From socket connection): %s", TimeTillOnConnectAfterConnection)

	greenTimestamp := fmt.Sprintf("Time until 'green': %s", TimeTillGreen)
	agentInitTimestamp := fmt.Sprintf("Time agent fully initialised: %s", TimeTillAgentInit)

	// print to stdout
	fmt.Println("Benchmarks:")
	fmt.Println("----------------------------------")
	fmt.Println(initCompletionTimestamp)
	fmt.Println()
	fmt.Println(connectionCompletionTimestamp)
	fmt.Println(connectionCompletionFromLaunchTimestamp)
	fmt.Println()
	fmt.Println(onConnectCompletionTimestamp)
	fmt.Println(onConnectAfterSocketCompletionTimestamp)
	fmt.Println()
	fmt.Println(greenTimestamp)
	fmt.Println(agentInitTimestamp)
	fmt.Println("----------------------------------")

	// log to file
	log.Info().Msg(initCompletionTimestamp)
	log.Info().Msg(connectionCompletionTimestamp)
	log.Info().Msg(connectionCompletionFromLaunchTimestamp)
	log.Info().Msg(onConnectCompletionTimestamp)
	log.Info().Msg(onConnectAfterSocketCompletionTimestamp)
	log.Info().Msg(agentInitTimestamp)
}
