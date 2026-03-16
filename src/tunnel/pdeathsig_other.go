//go:build !linux

package tunnel

import "os/exec"

func setPdeathsig(_ *exec.Cmd) {}
