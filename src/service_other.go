//go:build !windows

package main

import (
	"fmt"
	"os"
	"reagent/config"
)

// runningAsService reports whether the process was started by the Windows
// service control manager. Always false off Windows: on Linux the agent runs
// under systemd as a plain process.
func runningAsService() bool {
	return false
}

// runService hosts the agent as a Windows service; unreachable off Windows.
func runService(cliArgs *config.CommandLineArguments) {
	panic("runService is only available on Windows")
}

// runServiceCommand handles `reagent service <verb>`; Windows-only.
func runServiceCommand(args []string) int {
	fmt.Fprintln(os.Stderr, "the 'service' subcommand is only supported on Windows")
	return 1
}
