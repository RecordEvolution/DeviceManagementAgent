package main

// OS-neutral logic for the `reagent service` subcommand: flag parsing, the
// migration decision, and generated file/argument contents. Kept free of
// syscalls so it is unit-testable on every platform; the Windows-only pieces
// live in servicecmd_windows.go.

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"reagent/config"
	"strings"
)

const (
	serviceDisplayName = "IronFlock Device Agent"
	serviceDescription = "Connects this device to the IronFlock platform, runs apps via Docker, and keeps the agent up to date."
	repairTaskName     = "IronFlock Agent Repair"
	serviceConfigName  = "device.flock"
)

// certImportArgs builds `certutil -addstore -f <store> <certPath>` args to
// import our root into a device trust store. Root store makes the self-signed
// chain validate; TrustedPublisher makes UAC show a verified publisher and
// enables WDAC/AppLocker publisher rules.
func certImportArgs(store, certPath string) []string {
	return []string{"-addstore", "-f", store, certPath}
}

// certDeleteArgs builds `certutil -delstore <store> <name>` args to remove our
// imported root at uninstall (leaving a self-signed root behind is a lasting
// liability).
func certDeleteArgs(store, name string) []string {
	return []string{"-delstore", store, name}
}

// defenderAddExclusionCmd is the PowerShell that excludes exactly the frpc
// binary from Defender. frp is flagged as a dual-use tool (PUA); the narrow
// exclusion is the deterministic fix on devices we administer. Best-effort:
// Tamper Protection / Intune-managed devices ignore it.
func defenderAddExclusionCmd(frpcPath string) string {
	return "Add-MpPreference -ExclusionPath '" + frpcPath + "'"
}

// defenderRemoveExclusionCmd reverses the exclusion at uninstall.
func defenderRemoveExclusionCmd(frpcPath string) string {
	return "Remove-MpPreference -ExclusionPath '" + frpcPath + "'"
}

// agentDirFromImagePath extracts the -agentDir value the installer baked into
// the service's ImagePath, so uninstall can reverse the dir-scoped side
// effects (Defender exclusion, cert file). Returns "" when not found.
func agentDirFromImagePath(imagePath string) string {
	fields := splitCommandLine(imagePath)
	for i, f := range fields {
		if f == "-agentDir" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// splitCommandLine splits a Windows ImagePath into fields, honoring
// double-quoted segments (paths under %ProgramData% have no spaces by default,
// but a custom -agentDir might).
func splitCommandLine(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

type serviceInstallOptions struct {
	ConfigPath  string
	AgentDir    string
	AppsDir     string
	Proxy       string
	StartNow    bool
	AgentDirSet bool
}

// parseServiceInstallFlags parses `reagent service install` flags.
// programData is the value of %ProgramData% (injected for tests).
func parseServiceInstallFlags(args []string, programData string, output io.Writer) (*serviceInstallOptions, error) {
	flags := flag.NewFlagSet("service install", flag.ContinueOnError)
	flags.SetOutput(output)

	configPath := flags.String("config", "", "path to the device's .flock configuration file (required)")
	agentDir := flags.String("agentDir", "", "agent directory (default: %ProgramData%\\IronFlock\\Reagent)")
	appsDir := flags.String("appsDir", "", "apps directory (default: <agentDir>\\apps)")
	proxy := flags.String("proxy", "", "optional HTTP(S) proxy URL, written to the service environment")
	startNow := flags.Bool("start", false, "start the service right after installing")

	err := flags.Parse(args)
	if err != nil {
		return nil, err
	}

	if *configPath == "" {
		return nil, fmt.Errorf("-config <path-to-.flock> is required")
	}

	opts := serviceInstallOptions{
		ConfigPath:  *configPath,
		AgentDirSet: *agentDir != "",
		Proxy:       *proxy,
		StartNow:    *startNow,
	}

	opts.AgentDir = *agentDir
	if opts.AgentDir == "" {
		if programData == "" {
			return nil, fmt.Errorf("%%ProgramData%% is not set and no -agentDir was given")
		}
		opts.AgentDir = filepath.Join(programData, "IronFlock", "Reagent")
	}

	opts.AppsDir = *appsDir
	if opts.AppsDir == "" {
		opts.AppsDir = filepath.Join(opts.AgentDir, "apps")
	}

	return &opts, nil
}

// installCliArgs builds the CommandLineArguments the installer needs for
// filesystem.InitDirectories, mirroring how config.GetCliArguments derives
// sub-directories from -agentDir/-appsDir.
func installCliArgs(opts *serviceInstallOptions) *config.CommandLineArguments {
	return &config.CommandLineArguments{
		AgentDir:       opts.AgentDir,
		AppsDirectory:  opts.AppsDir,
		AppsBuildDir:   opts.AppsDir + "/build",
		AppsComposeDir: opts.AppsDir + "/compose",
		AppsSharedDir:  opts.AppsDir + "/shared",
		DownloadDir:    opts.AgentDir + "/downloads",
	}
}

// serviceBinaryArgs are the arguments baked into the service ImagePath. Every
// path is explicit so the os.UserHomeDir-based defaults (meaningless under
// LocalSystem) never apply at service runtime.
func serviceBinaryArgs(opts *serviceInstallOptions) []string {
	return []string{
		"-config", filepath.Join(opts.AgentDir, serviceConfigName),
		"-agentDir", opts.AgentDir,
		"-appsDir", opts.AppsDir,
		"-dbFileName", filepath.Join(opts.AgentDir, "reagent.db"),
		"-logFile", filepath.Join(opts.AgentDir, "reagent.log"),
	}
}

// migrationAbortMessage explains the choice when data from a previous manual
// (console) installation exists: reuse it, or start fresh. Moving the data is
// deliberately not offered — app /data bind-mount sources are baked into the
// existing containers, so a silent move would strand their data.
func migrationAbortMessage(existingDirs []string, defaultAgentDir string) string {
	var b strings.Builder
	b.WriteString("Found data from a previous agent installation:\n")
	for _, dir := range existingDirs {
		b.WriteString("  " + dir + "\n")
	}
	b.WriteString("\nRe-run with an explicit -agentDir to choose:\n")
	b.WriteString("  keep using the existing device data:  -agentDir \"" + existingDirs[0] + "\"\n")
	b.WriteString("  start fresh (existing app data stays behind):  -agentDir \"" + defaultAgentDir + "\"\n")
	return b.String()
}

// repairCmdContent is the boot-time repair script registered as an ONSTART
// scheduled task. It closes the one gap in-process code cannot: an
// interrupted self-update swap that left the service ImagePath vacant (SCM
// recovery actions do not fire for a service that fails to START). Prefers
// the known-good previous binary, falls back to the newest downloaded one.
func repairCmdContent(agentDir string) string {
	return strings.Join([]string{
		"@echo off",
		"rem IronFlock agent repair (registered as scheduled task '" + repairTaskName + "')",
		"rem Restores the service binary if an interrupted self-update left it missing.",
		"set \"AGENTDIR=" + agentDir + "\"",
		"if exist \"%AGENTDIR%\\reagent.exe\" goto start",
		"if exist \"%AGENTDIR%\\reagent-prev.exe\" (",
		"  ren \"%AGENTDIR%\\reagent-prev.exe\" \"reagent.exe\"",
		"  goto start",
		")",
		"for /f \"delims=\" %%F in ('dir /b /o-d \"%AGENTDIR%\\reagent-v*.exe\" 2^>nul') do (",
		"  ren \"%AGENTDIR%\\%%F\" \"reagent.exe\"",
		"  goto start",
		")",
		":start",
		"sc start reagent",
		"",
	}, "\r\n")
}
