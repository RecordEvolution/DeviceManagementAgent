//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reagent/codesign"
	"reagent/filesystem"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// installTrustAnchors writes every embedded code-signing root cert to the
// agent dir and imports each into the machine trust stores: Trusted Root (so
// the self-signed signature chain validates) and TrustedPublisher (so UAC
// shows a verified publisher and WDAC/AppLocker publisher rules can allow it).
// More than one root during a rotation's overlap window. No-ops when no real
// root is embedded yet (pre-signing transition). All failures are warnings —
// the service runs regardless.
func installTrustAnchors(agentDir string) {
	for _, root := range codesign.EmbeddedRoots() {
		certPath := filepath.Join(agentDir, root.FileName)
		if err := os.WriteFile(certPath, root.PEM, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write code-signing root %q: %v\n", root.FileName, err)
			continue
		}
		for _, store := range []string{"Root", "TrustedPublisher"} {
			if err := runCommand("certutil", certImportArgs(store, certPath)...); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not import %q into %s: %v\n", root.FileName, store, err)
			}
		}
	}
}

// removeTrustAnchors reverses installTrustAnchors at uninstall — leaving a
// self-signed root in Trusted Root is a lasting liability. Removes every
// embedded root by its common name.
func removeTrustAnchors(agentDir string) {
	for _, root := range codesign.EmbeddedRoots() {
		for _, store := range []string{"Root", "TrustedPublisher"} {
			if err := runCommand("certutil", certDeleteArgs(store, root.CommonName)...); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not remove %q from %s: %v\n", root.CommonName, store, err)
			}
		}
		_ = os.Remove(filepath.Join(agentDir, root.FileName))
	}
}

// Language-neutral SIDs (localized group names like "Administratoren" would
// break icacls on non-English Windows).
const (
	sidSystem         = "*S-1-5-18"
	sidAdministrators = "*S-1-5-32-544"
	sidUsers          = "*S-1-5-32-545"
)

func runServiceCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reagent service <install|uninstall|start|stop|status>")
		return 2
	}

	var err error
	switch args[0] {
	case "install":
		err = serviceInstall(args[1:])
	case "uninstall":
		err = serviceUninstall()
	case "start":
		err = serviceControl(func(s *mgr.Service) error { return s.Start() })
	case "stop":
		err = serviceStop()
	case "status":
		err = serviceStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown service command %q\nusage: reagent service <install|uninstall|start|stop|status>\n", args[0])
		return 2
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func requireElevation() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("this command must be run from an elevated (Administrator) prompt")
	}
	return nil
}

func serviceInstall(args []string) error {
	err := requireElevation()
	if err != nil {
		return err
	}

	opts, err := parseServiceInstallFlags(args, os.Getenv("ProgramData"), os.Stderr)
	if err != nil {
		return err
	}

	// Data from a previous manual installation must not be silently orphaned:
	// force an explicit -agentDir decision when any is found.
	if !opts.AgentDirSet {
		existingDirs := findExistingAgentDirs()
		if len(existingDirs) > 0 {
			return fmt.Errorf("%s", migrationAbortMessage(existingDirs, opts.AgentDir))
		}
	}

	_, err = os.Stat(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("cannot read the .flock config file: %w", err)
	}

	err = filesystem.InitDirectories(installCliArgs(opts))
	if err != nil {
		return fmt.Errorf("failed to create agent directories: %w", err)
	}

	// Harden the agent dir: the service self-update renames-and-executes
	// binaries from here as SYSTEM, and the .flock/.env-compose files contain
	// the device secret — regular users get no access at all. The apps dir
	// gets an explicit modify grant back: Docker Desktop bind mounts access
	// app /data in the interactive user's context.
	err = runCommand("icacls", opts.AgentDir, "/inheritance:r",
		"/grant", sidSystem+":(OI)(CI)F",
		"/grant", sidAdministrators+":(OI)(CI)F")
	if err != nil {
		return fmt.Errorf("failed to restrict permissions on %s: %w", opts.AgentDir, err)
	}
	err = runCommand("icacls", opts.AppsDir, "/grant", sidUsers+":(OI)(CI)M")
	if err != nil {
		return fmt.Errorf("failed to grant app-data permissions on %s: %w", opts.AppsDir, err)
	}

	installedExe := filepath.Join(opts.AgentDir, "reagent.exe")
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	err = copyFileIfDifferent(currentExe, installedExe)
	if err != nil {
		return fmt.Errorf("failed to install the agent binary: %w", err)
	}

	installedConfig := filepath.Join(opts.AgentDir, serviceConfigName)
	err = copyFileIfDifferent(opts.ConfigPath, installedConfig)
	if err != nil {
		return fmt.Errorf("failed to install the .flock config: %w", err)
	}

	repairCmd := filepath.Join(opts.AgentDir, "repair.cmd")
	err = os.WriteFile(repairCmd, []byte(repairCmdContent(opts.AgentDir)), 0644)
	if err != nil {
		return fmt.Errorf("failed to write repair.cmd: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to the service manager: %w", err)
	}
	defer m.Disconnect()

	existing, err := m.OpenService(serviceName)
	if err == nil {
		existing.Close()
		return fmt.Errorf("service %q is already installed — run 'reagent service uninstall' first", serviceName)
	}

	service, err := m.CreateService(serviceName, installedExe, mgr.Config{
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		ErrorControl:     mgr.ErrorNormal,
	}, serviceBinaryArgs(opts)...)
	if err != nil {
		return fmt.Errorf("failed to create the service: %w", err)
	}
	defer service.Close()

	// Restart on every failure, never give up: the last action repeats when
	// the list is exhausted, so the 120s delay throttles a crash loop while
	// deliberate exit-for-restart (updates, remote restart) recovers in 5s.
	err = service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 120 * time.Second},
	}, 86400)
	if err != nil {
		return fmt.Errorf("failed to set service recovery actions: %w", err)
	}
	// Also fire recovery when the process reports STOPPED with a non-zero
	// exit code (belt and braces next to the crash-style exits).
	err = service.SetRecoveryActionsOnNonCrashFailures(true)
	if err != nil {
		return fmt.Errorf("failed to enable recovery on non-crash failures: %w", err)
	}

	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil && !strings.Contains(err.Error(), "registry key already exists") {
		fmt.Fprintf(os.Stderr, "warning: could not register the event log source: %v\n", err)
	}

	// Boot-time repair task: SCM recovery cannot fix a service whose binary
	// is missing (start failures are not service failures), the scheduled
	// task can.
	err = runCommand("schtasks", "/Create", "/F",
		"/TN", repairTaskName,
		"/SC", "ONSTART",
		"/RU", "SYSTEM",
		"/TR", "\""+repairCmd+"\"")
	if err != nil {
		return fmt.Errorf("failed to register the repair scheduled task: %w", err)
	}

	if opts.Proxy != "" {
		err = setServiceProxy(opts.Proxy)
		if err != nil {
			return fmt.Errorf("failed to set the service proxy environment: %w", err)
		}
	}

	// Import our code-signing root so the agent's signed self-updates are
	// trusted (UAC verified publisher; on-device pinning). No-ops until a real
	// root is embedded. Non-fatal — an un-imported cert only weakens the
	// verified-publisher UX, it doesn't stop the service.
	installTrustAnchors(opts.AgentDir)

	// Exclude frpc.exe from Defender (frp is flagged PUA). Best-effort;
	// Tamper Protection / managed policy may ignore it.
	frpcPath := filepath.Join(opts.AgentDir, "frpc.exe")
	err = runCommand("powershell", "-NonInteractive", "-Command", defenderAddExclusionCmd(frpcPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not add the Defender exclusion for frpc (tunnels may be quarantined on managed devices): %v\n", err)
	}

	fmt.Printf("Installed service %q (agent dir: %s)\n", serviceName, opts.AgentDir)

	if opts.StartNow {
		err = service.Start()
		if err != nil {
			return fmt.Errorf("installed, but failed to start the service: %w", err)
		}
		fmt.Println("Service started.")
	} else {
		fmt.Println("Start it with: reagent service start")
	}

	return nil
}

func serviceUninstall() error {
	err := requireElevation()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to the service manager: %w", err)
	}
	defer m.Disconnect()

	service, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", serviceName)
	}
	defer service.Close()

	// Recover the agent dir from the baked ImagePath before deleting, so the
	// dir-scoped side effects (Defender exclusion, cert file) can be reversed.
	agentDir := ""
	if cfg, cfgErr := service.Config(); cfgErr == nil {
		agentDir = agentDirFromImagePath(cfg.BinaryPathName)
	}

	status, err := service.Control(svc.Stop)
	if err == nil {
		// wait for the stop to complete before deleting
		for i := 0; i < 30 && status.State != svc.Stopped; i++ {
			time.Sleep(time.Second)
			status, err = service.Query()
			if err != nil {
				break
			}
		}
	}

	err = service.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete the service: %w", err)
	}

	err = eventlog.Remove(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove the event log source: %v\n", err)
	}

	err = runCommand("schtasks", "/Delete", "/F", "/TN", repairTaskName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove the repair scheduled task: %v\n", err)
	}

	// Reverse the install-time trust + AV side effects (symmetric uninstall).
	if agentDir != "" {
		frpcPath := filepath.Join(agentDir, "frpc.exe")
		if rmErr := runCommand("powershell", "-NonInteractive", "-Command", defenderRemoveExclusionCmd(frpcPath)); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove the Defender exclusion: %v\n", rmErr)
		}
		removeTrustAnchors(agentDir)
	}

	fmt.Printf("Uninstalled service %q. The agent directory and app data were kept.\n", serviceName)
	return nil
}

func serviceStop() error {
	return serviceControl(func(s *mgr.Service) error {
		status, err := s.Control(svc.Stop)
		if err != nil {
			return err
		}
		for i := 0; i < 30 && status.State != svc.Stopped; i++ {
			time.Sleep(time.Second)
			status, err = s.Query()
			if err != nil {
				return err
			}
		}
		if status.State != svc.Stopped {
			return fmt.Errorf("service did not stop in time")
		}
		return nil
	})
}

func serviceStatus() error {
	return serviceControl(func(s *mgr.Service) error {
		status, err := s.Query()
		if err != nil {
			return err
		}

		stateNames := map[svc.State]string{
			svc.Stopped:         "stopped",
			svc.StartPending:    "start pending",
			svc.StopPending:     "stop pending",
			svc.Running:         "running",
			svc.ContinuePending: "continue pending",
			svc.PausePending:    "pause pending",
			svc.Paused:          "paused",
		}
		name, ok := stateNames[status.State]
		if !ok {
			name = fmt.Sprintf("unknown (%d)", status.State)
		}
		fmt.Println(name)
		return nil
	})
}

func serviceControl(action func(*mgr.Service) error) error {
	err := requireElevation()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to the service manager: %w", err)
	}
	defer m.Disconnect()

	service, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", serviceName)
	}
	defer service.Close()

	return action(service)
}

// findExistingAgentDirs locates data from previous manual (console)
// installations. The installer runs elevated, so %USERPROFILE% would be the
// admin's profile — scan every user profile instead. FlockFlasher's
// throwaway test-device dir (~\.Reflasher\agent) is deliberately not matched.
func findExistingAgentDirs() []string {
	systemDrive := os.Getenv("SystemDrive")
	if systemDrive == "" {
		systemDrive = "C:"
	}

	matches, err := filepath.Glob(filepath.Join(systemDrive+`\`, "Users", "*", "reagent", "reagent.db"))
	if err != nil {
		return nil
	}

	dirs := make([]string, 0, len(matches))
	for _, match := range matches {
		dirs = append(dirs, filepath.Dir(match))
	}
	return dirs
}

// setServiceProxy writes the proxy into the service's Environment registry
// value (REG_MULTI_SZ). A LocalSystem service only inherits machine-wide
// environment, and Go does not read the WinHTTP/IE proxy settings.
func setServiceProxy(proxy string) error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+serviceName, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	return key.SetStringsValue("Environment", []string{
		"HTTP_PROXY=" + proxy,
		"HTTPS_PROXY=" + proxy,
		"NO_PROXY=localhost,127.0.0.1",
	})
}

func runCommand(name string, args ...string) error {
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyFileIfDifferent(src string, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if strings.EqualFold(srcAbs, dstAbs) {
		return nil
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	// Write to a temp name first so a torn copy never leaves a half-written
	// file at the final path.
	tmp := dst + ".tmp"
	destination, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	_, err = io.Copy(destination, source)
	closeErr := destination.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}

	return os.Rename(tmp, dst)
}
