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
