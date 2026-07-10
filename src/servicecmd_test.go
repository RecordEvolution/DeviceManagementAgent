package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseServiceInstallFlagsDefaults(t *testing.T) {
	opts, err := parseServiceInstallFlags([]string{"-config", "C:\\tmp\\dev.flock"}, `C:\ProgramData`, io.Discard)
	require.NoError(t, err)

	assert.Equal(t, "C:\\tmp\\dev.flock", opts.ConfigPath)
	assert.False(t, opts.AgentDirSet)
	assert.Equal(t, filepath.Join(`C:\ProgramData`, "IronFlock", "Reagent"), opts.AgentDir)
	assert.Equal(t, filepath.Join(opts.AgentDir, "apps"), opts.AppsDir)
	assert.False(t, opts.StartNow)
	assert.Empty(t, opts.Proxy)
}

func TestParseServiceInstallFlagsExplicit(t *testing.T) {
	opts, err := parseServiceInstallFlags([]string{
		"-config", "dev.flock",
		"-agentDir", `D:\data\reagent`,
		"-proxy", "http://proxy:3128",
		"-start",
	}, `C:\ProgramData`, io.Discard)
	require.NoError(t, err)

	assert.True(t, opts.AgentDirSet)
	assert.Equal(t, `D:\data\reagent`, opts.AgentDir)
	assert.Equal(t, filepath.Join(`D:\data\reagent`, "apps"), opts.AppsDir)
	assert.Equal(t, "http://proxy:3128", opts.Proxy)
	assert.True(t, opts.StartNow)
}

func TestParseServiceInstallFlagsRequiresConfig(t *testing.T) {
	_, err := parseServiceInstallFlags([]string{}, `C:\ProgramData`, io.Discard)
	require.ErrorContains(t, err, "-config")
}

func TestParseServiceInstallFlagsRequiresProgramData(t *testing.T) {
	_, err := parseServiceInstallFlags([]string{"-config", "dev.flock"}, "", io.Discard)
	require.ErrorContains(t, err, "ProgramData")

	// but an explicit -agentDir works without %ProgramData%
	opts, err := parseServiceInstallFlags([]string{"-config", "dev.flock", "-agentDir", `D:\reagent`}, "", io.Discard)
	require.NoError(t, err)
	assert.Equal(t, `D:\reagent`, opts.AgentDir)
}

// The ImagePath arguments must pin every path the agent would otherwise
// derive from os.UserHomeDir (meaningless under LocalSystem).
func TestServiceBinaryArgs(t *testing.T) {
	opts := &serviceInstallOptions{AgentDir: `C:\ProgramData\IronFlock\Reagent`, AppsDir: `C:\ProgramData\IronFlock\Reagent\apps`}
	args := serviceBinaryArgs(opts)

	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "-config "+filepath.Join(opts.AgentDir, serviceConfigName))
	assert.Contains(t, joined, "-agentDir "+opts.AgentDir)
	assert.Contains(t, joined, "-appsDir "+opts.AppsDir)
	assert.Contains(t, joined, "-dbFileName "+filepath.Join(opts.AgentDir, "reagent.db"))
	assert.Contains(t, joined, "-logFile "+filepath.Join(opts.AgentDir, "reagent.log"))
}

func TestInstallCliArgsDerivesSubdirs(t *testing.T) {
	opts := &serviceInstallOptions{AgentDir: `C:\agent`, AppsDir: `C:\agent\apps`}
	cliArgs := installCliArgs(opts)

	assert.Equal(t, `C:\agent`, cliArgs.AgentDir)
	assert.Equal(t, `C:\agent\apps`, cliArgs.AppsDirectory)
	assert.Equal(t, `C:\agent\apps/build`, cliArgs.AppsBuildDir)
	assert.Equal(t, `C:\agent\apps/compose`, cliArgs.AppsComposeDir)
	assert.Equal(t, `C:\agent\apps/shared`, cliArgs.AppsSharedDir)
	assert.Equal(t, `C:\agent/downloads`, cliArgs.DownloadDir)
}

func TestMigrationAbortMessageOffersBothChoices(t *testing.T) {
	message := migrationAbortMessage([]string{`C:\Users\alice\reagent`}, `C:\ProgramData\IronFlock\Reagent`)

	assert.Contains(t, message, `C:\Users\alice\reagent`)
	assert.Contains(t, message, `-agentDir "C:\Users\alice\reagent"`)
	assert.Contains(t, message, `-agentDir "C:\ProgramData\IronFlock\Reagent"`)
}

func TestRepairCmdContent(t *testing.T) {
	content := repairCmdContent(`C:\ProgramData\IronFlock\Reagent`)

	assert.Contains(t, content, `set "AGENTDIR=C:\ProgramData\IronFlock\Reagent"`)
	assert.Contains(t, content, `reagent-prev.exe`)
	assert.Contains(t, content, `reagent-v*.exe`)
	assert.Contains(t, content, "sc start reagent")
	assert.True(t, strings.HasSuffix(content, "\r\n"), "batch files want CRLF endings")
	for _, line := range strings.Split(content, "\r\n") {
		assert.NotContains(t, line, "\n", "no bare LF inside lines")
	}
}

func TestCertImportArgs(t *testing.T) {
	args := certImportArgs("Root", `C:\ProgramData\IronFlock\Reagent\ironflock-root.crt`)
	assert.Equal(t, []string{"-addstore", "-f", "Root", `C:\ProgramData\IronFlock\Reagent\ironflock-root.crt`}, args)

	pub := certImportArgs("TrustedPublisher", `C:\x\root.crt`)
	assert.Equal(t, "TrustedPublisher", pub[2])
}

func TestCertDeleteArgs(t *testing.T) {
	assert.Equal(t, []string{"-delstore", "Root", "IronFlock Code Signing Root"},
		certDeleteArgs("Root", "IronFlock Code Signing Root"))
}

func TestDefenderExclusionCmds(t *testing.T) {
	frpc := `C:\ProgramData\IronFlock\Reagent\frpc.exe`
	add := defenderAddExclusionCmd(frpc)
	assert.Contains(t, add, "Add-MpPreference -ExclusionPath")
	assert.Contains(t, add, frpc)

	rm := defenderRemoveExclusionCmd(frpc)
	assert.Contains(t, rm, "Remove-MpPreference -ExclusionPath")
	assert.Contains(t, rm, frpc)
}

func TestAgentDirFromImagePath(t *testing.T) {
	// Unquoted (default ProgramData path, no spaces).
	img := `C:\ProgramData\IronFlock\Reagent\reagent.exe -config C:\ProgramData\IronFlock\Reagent\device.flock -agentDir C:\ProgramData\IronFlock\Reagent -appsDir C:\ProgramData\IronFlock\Reagent\apps`
	assert.Equal(t, `C:\ProgramData\IronFlock\Reagent`, agentDirFromImagePath(img))

	// Quoted path with a space.
	imgQuoted := `"C:\Program Files\IronFlock\reagent.exe" -agentDir "D:\my data\reagent" -logFile "D:\my data\reagent\reagent.log"`
	assert.Equal(t, `D:\my data\reagent`, agentDirFromImagePath(imgQuoted))

	// Missing -agentDir.
	assert.Equal(t, "", agentDirFromImagePath(`C:\x\reagent.exe -config c.flock`))
}
