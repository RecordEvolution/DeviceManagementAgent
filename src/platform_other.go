//go:build !windows

package main

// acquireSingleInstanceLock guards against two agents on one machine. Only
// needed on Windows (service + console double-run); on Linux systemd owns the
// single reagent instance.
func acquireSingleInstanceLock() error {
	return nil
}

// setupProcessJobObject puts the process into a kill-on-close Windows job
// object so children (docker compose CLI) die with the agent. On Linux the
// same is achieved per-child via Pdeathsig (container/compose_pdeathsig_linux.go).
func setupProcessJobObject() {
}
