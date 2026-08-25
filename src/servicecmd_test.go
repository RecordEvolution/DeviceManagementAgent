package main

import (
	"encoding/json"
	"io"
	"path/filepath"
	"reagent/config"
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
	assert.True(t, opts.StartNow, "installing must start the service by default")
	assert.Empty(t, opts.Proxy)
}

// -start predates the start-by-default behaviour and is passed by every published
// doc and provisioning script, so it must keep parsing (as a no-op).
func TestParseServiceInstallFlagsStartIsAcceptedNoOp(t *testing.T) {
	opts, err := parseServiceInstallFlags([]string{"-config", "dev.flock", "-start"}, `C:\ProgramData`, io.Discard)
	require.NoError(t, err)
	assert.True(t, opts.StartNow)
}

func TestParseServiceInstallFlagsNoStart(t *testing.T) {
	opts, err := parseServiceInstallFlags([]string{"-config", "dev.flock", "-noStart"}, `C:\ProgramData`, io.Discard)
	require.NoError(t, err)
	assert.False(t, opts.StartNow)

	// -noStart wins even when the legacy -start is also present.
	opts, err = parseServiceInstallFlags([]string{"-config", "dev.flock", "-start", "-noStart"}, `C:\ProgramData`, io.Discard)
	require.NoError(t, err)
	assert.False(t, opts.StartNow)
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

func TestParseServiceInstallFlagsRejectsUnknown(t *testing.T) {
	_, err := parseServiceInstallFlags([]string{"-config", "dev.flock", "-bogus"}, `C:\ProgramData`, io.Discard)
	require.Error(t, err)
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

// Appliance-issued device flocks list only the appliance's *local* registry
// names; the externally reachable registry comes from docker_registry_url and
// must be merged in.
func TestInsecureRegistryEntriesApplianceDeviceFlock(t *testing.T) {
	entries := insecureRegistryEntries(&config.ReswarmConfig{
		InsecureRegistries: `["localhost:15001", "appstore-registry:5000"]`,
		DockerRegistryURL:  "136.230.111.59:15001/",
	})
	assert.Equal(t, []string{"localhost:15001", "appstore-registry:5000", "136.230.111.59:15001"}, entries)
}

// The appliance installer writes the agent's own flock with a bare
// `host:port/` string (not a JSON array); it duplicates docker_registry_url.
func TestInsecureRegistryEntriesBareLegacyString(t *testing.T) {
	entries := insecureRegistryEntries(&config.ReswarmConfig{
		InsecureRegistries: "136.230.111.59:15001/",
		DockerRegistryURL:  "136.230.111.59:15001/",
	})
	assert.Equal(t, []string{"136.230.111.59:15001"}, entries)
}

// Cloud and domain-mode registries are HTTPS with no explicit port — nothing
// must be marked insecure for them.
func TestInsecureRegistryEntriesHTTPSRegistries(t *testing.T) {
	assert.Empty(t, insecureRegistryEntries(&config.ReswarmConfig{
		DockerRegistryURL: "registry.ironflock.com/",
	}))
	assert.Empty(t, insecureRegistryEntries(&config.ReswarmConfig{
		DockerRegistryURL: "registry.customer.example/",
	}))
}

func TestMergeInsecureRegistriesCreatesFromEmpty(t *testing.T) {
	merged, changed, err := mergeInsecureRegistries(nil, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.True(t, changed)

	var daemonCfg map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &daemonCfg))
	assert.Equal(t, []interface{}{"136.230.111.59:15001"}, daemonCfg["insecure-registries"])
}

// Docker Desktop machines commonly have a daemon.json already — other keys and
// existing entries must survive the merge untouched.
func TestMergeInsecureRegistriesPreservesExisting(t *testing.T) {
	existing := []byte(`{
  "builder": {"gc": {"enabled": true}},
  "insecure-registries": ["hubproxy.docker.internal:5555"]
}`)
	merged, changed, err := mergeInsecureRegistries(existing, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.True(t, changed)

	var daemonCfg map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &daemonCfg))
	assert.Equal(t, []interface{}{"hubproxy.docker.internal:5555", "136.230.111.59:15001"}, daemonCfg["insecure-registries"])
	assert.Contains(t, daemonCfg, "builder")
}

// A rerun (or an operator who already added the entry by hand) must be a
// byte-for-byte no-op.
func TestMergeInsecureRegistriesIdempotent(t *testing.T) {
	existing := []byte(`{"insecure-registries": ["136.230.111.59:15001"]}`)
	merged, changed, err := mergeInsecureRegistries(existing, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, existing, merged)
}

// A hand-edited daemon.json that no longer parses must never be clobbered.
func TestMergeInsecureRegistriesRejectsMalformed(t *testing.T) {
	_, _, err := mergeInsecureRegistries([]byte(`{"insecure-registries": [`), []string{"x:1"})
	assert.Error(t, err)
}

// The registry token is used as the docker *username* and is far longer than
// Windows Credential Manager accepts, so the app registry must be opted out of
// the credential helper or every login fails at the store step.
func TestMergeCredentialHelperOptOutCreatesEntry(t *testing.T) {
	merged, changed, err := mergeCredentialHelperOptOut(nil, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.True(t, changed)

	var cfg map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &cfg))
	helpers := cfg["credHelpers"].(map[string]interface{})
	assert.Equal(t, "", helpers["136.230.111.59:15001"])
}

// Docker Desktop machines have a config.json already; auths, contexts and any
// other keys must survive untouched.
func TestMergeCredentialHelperOptOutPreservesConfig(t *testing.T) {
	existing := []byte(`{"auths":{"registry.example":{"auth":"x"}},"currentContext":"desktop-linux"}`)
	merged, changed, err := mergeCredentialHelperOptOut(existing, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.True(t, changed)

	var cfg map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &cfg))
	assert.Contains(t, cfg, "auths")
	assert.Equal(t, "desktop-linux", cfg["currentContext"])
	assert.Equal(t, "", cfg["credHelpers"].(map[string]interface{})["136.230.111.59:15001"])
}

// An operator who deliberately configured a helper for this registry keeps it.
func TestMergeCredentialHelperOptOutKeepsOperatorChoice(t *testing.T) {
	existing := []byte(`{"credHelpers":{"136.230.111.59:15001":"wincred"}}`)
	merged, changed, err := mergeCredentialHelperOptOut(existing, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, existing, merged)
}

func TestMergeCredentialHelperOptOutIdempotent(t *testing.T) {
	existing := []byte(`{"credHelpers":{"136.230.111.59:15001":""}}`)
	_, changed, err := mergeCredentialHelperOptOut(existing, []string{"136.230.111.59:15001"})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestMergeCredentialHelperOptOutRejectsMalformed(t *testing.T) {
	_, _, err := mergeCredentialHelperOptOut([]byte(`{"credHelpers":`), []string{"x:1"})
	assert.Error(t, err)
}
