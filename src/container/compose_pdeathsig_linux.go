//go:build linux

package container

import (
	"os/exec"
	"syscall"
)

// setPdeathsig configures the child process to receive SIGKILL when the parent
// agent process dies, preventing orphaned compose subprocesses on agent restart.
func setPdeathsig(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}
}
