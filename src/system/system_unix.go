//go:build !windows

package system

import "os/exec"

func (sys *System) Reboot() error {
	_, err := exec.Command("reboot").Output()
	return err
}

func (sys *System) Poweroff() error {
	_, err := exec.Command("poweroff").Output()
	return err
}

func (sys *System) RestartAgent() error {
	_, err := exec.Command("systemctl", "restart", "reagent").Output()
	return err
}
