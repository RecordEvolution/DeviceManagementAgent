//go:build windows

package system

import (
	"os/exec"
	"reagent/lifecycle"
)

// The 5-second delay mirrors the Linux handlers' grace period and lets the
// WAMP response reach the caller before the machine goes down.

func (sys *System) Reboot() error {
	_, err := exec.Command("shutdown", "/r", "/t", "5").Output()
	return err
}

func (sys *System) Poweroff() error {
	_, err := exec.Command("shutdown", "/s", "/t", "5").Output()
	return err
}

// RestartAgent requests a restart from the service control loop. In console
// mode there is no supervisor, so this returns ErrNotSupervised, which the
// system_restart_agent handler surfaces to the caller.
func (sys *System) RestartAgent() error {
	return lifecycle.RequestRestart("system_restart_agent")
}
