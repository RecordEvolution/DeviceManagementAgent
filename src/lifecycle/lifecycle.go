// Package lifecycle brokers process-restart requests between the components
// that need one (self-update activation, the system_restart_agent handler) and
// whatever supervises the process. On Linux the supervisor is external
// (systemd + reagent-manager.sh), so nothing subscribes and RestartAgent execs
// systemctl directly. Under the Windows service the SCM control loop
// subscribes and turns each request into an exit-for-restart.
package lifecycle

import (
	"errors"
	"sync"
)

var (
	mu       sync.Mutex
	requests chan string
)

// ErrNotSupervised is returned by RequestRestart when no supervisor is
// subscribed: in console mode there is nothing that would start the process
// again, so callers must surface this instead of exiting.
var ErrNotSupervised = errors.New("no process supervisor available: the agent is not running as a service")

// Subscribe marks the process as supervised and returns the channel restart
// requests are delivered on. Called once by the service control loop.
func Subscribe() <-chan string {
	mu.Lock()
	defer mu.Unlock()

	if requests == nil {
		requests = make(chan string, 1)
	}
	return requests
}

// Supervised reports whether a supervisor is subscribed.
func Supervised() bool {
	mu.Lock()
	defer mu.Unlock()

	return requests != nil
}

// RequestRestart asks the supervisor to restart the process. A request that
// arrives while one is already pending is coalesced into it. Returns
// ErrNotSupervised when nothing is subscribed.
func RequestRestart(reason string) error {
	mu.Lock()
	defer mu.Unlock()

	if requests == nil {
		return ErrNotSupervised
	}

	select {
	case requests <- reason:
	default: // a restart is already pending; it covers this request too
	}
	return nil
}
