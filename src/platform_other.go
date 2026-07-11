//go:build !windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// singleInstanceLockFile is held open (and flock'ed) for the process lifetime;
// the kernel releases the lock on any exit, including SIGKILL.
var singleInstanceLockFile *os.File

const singleInstanceLockPath = "/tmp/ironflock-reagent.lock"

// errAgentAlreadyRunning mirrors the Windows message: the most likely holder
// of the lock is the installed system service.
func errAgentAlreadyRunning() error {
	return fmt.Errorf("another reagent instance is already running on this machine (if it runs as a service, stop it with: systemctl stop reagent)")
}

// acquireSingleInstanceLock refuses a second agent on the machine: two agents
// treat each other's containers as orphans and delete them
// (apps.CleanupOrphanedContainers) — the same hazard the Global\ mutex guards
// against on Windows (platform_windows.go). Self-update is unaffected: it
// restarts via systemd (system.RestartAgent), so the old process exits —
// releasing the lock — before the new one starts.
func acquireSingleInstanceLock() error {
	for {
		// O_RDONLY is enough for flock(2) and keeps the file openable by any
		// user regardless of which user created it (root service vs console).
		f, err := os.OpenFile(singleInstanceLockPath, os.O_RDONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("cannot open the machine-wide agent lock %s: %w", singleInstanceLockPath, err)
		}

		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == unix.EWOULDBLOCK {
			f.Close()
			return errAgentAlreadyRunning()
		}
		if err != nil {
			f.Close()
			return fmt.Errorf("cannot lock the machine-wide agent lock %s: %w", singleInstanceLockPath, err)
		}

		// The path may have been unlinked or replaced between open and flock
		// (systemd-tmpfiles aging, an operator's rm): a lock on an unlinked
		// inode guards nothing, so verify the locked fd is still what the
		// path names and start over if not.
		var fdStat, pathStat unix.Stat_t
		if unix.Fstat(int(f.Fd()), &fdStat) == nil &&
			unix.Stat(singleInstanceLockPath, &pathStat) == nil &&
			fdStat.Ino == pathStat.Ino && fdStat.Dev == pathStat.Dev {
			singleInstanceLockFile = f
			return nil
		}
		f.Close()
	}
}

// setupProcessJobObject puts the process into a kill-on-close Windows job
// object so children (docker compose CLI) die with the agent. On Linux the
// same is achieved per-child via Pdeathsig (container/compose_pdeathsig_linux.go).
func setupProcessJobObject() {
}
