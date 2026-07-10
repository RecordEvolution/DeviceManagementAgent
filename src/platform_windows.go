//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

// singleInstanceMutex is held for the process lifetime; never closed.
var singleInstanceMutex windows.Handle

// acquireSingleInstanceLock refuses a second agent on the machine. The mutex
// lives in the Global\ namespace so it spans session 0 (the service) and
// interactive sessions: a leftover console agent and the service would
// otherwise treat each other's containers as orphans and delete them
// (apps.CleanupOrphanedContainers). This also blocks FlockFlasher's
// "Test Device" console agent on machines that run the service — intended.
func acquireSingleInstanceLock() error {
	name, err := windows.UTF16PtrFromString(`Global\ironflock-reagent`)
	if err != nil {
		return err
	}

	// The x/sys wrapper reports ERROR_ALREADY_EXISTS as err even though the
	// call returns a valid handle in that case.
	handle, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		return fmt.Errorf("another reagent instance is already running on this machine (if it runs as a service, stop it with: reagent service stop)")
	}
	if err != nil {
		return fmt.Errorf("failed to create single-instance mutex: %w", err)
	}

	singleInstanceMutex = handle
	return nil
}

// setupProcessJobObject puts the agent into a kill-on-close job object so
// every child process (docker compose CLI invocations) dies with the agent —
// the Windows equivalent of the Pdeathsig used on Linux
// (container/compose_pdeathsig_linux.go). Failures are logged, not fatal:
// the agent works without it, children just may outlive a hard kill.
func setupProcessJobObject() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to create job object for child process cleanup")
		return
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to configure job object for child process cleanup")
		windows.CloseHandle(job)
		return
	}

	err = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
	if err != nil {
		log.Error().Err(err).Msg("failed to assign the agent to its job object")
		windows.CloseHandle(job)
		return
	}
	// The job handle is deliberately kept open for the process lifetime;
	// closing it would kill the whole job (including this process).
}
