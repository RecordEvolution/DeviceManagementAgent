//go:build !linux

package container

import "os/exec"

// setPdeathsig is a no-op on non-Linux platforms (e.g. macOS dev builds).
func setPdeathsig(_ *exec.Cmd) {}
