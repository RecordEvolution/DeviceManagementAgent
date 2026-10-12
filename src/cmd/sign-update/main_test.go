package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"reagent/updatesig"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keygenForTest runs keygen into a temp dir and returns the PEM path, the
// private key read back from it and keygen's output. The key exists for this
// test only.
func keygenForTest(t *testing.T) (string, ed25519.PrivateKey, string) {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "agent-update-signing.pem")
	var stdout bytes.Buffer
	require.NoError(t, run([]string{"keygen", "-out", keyPath}, &stdout))

	data, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	block, _ := pem.Decode(data)
	require.NotNil(t, block)
	require.Equal(t, "PRIVATE KEY", block.Type)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	privateKey, ok := parsed.(ed25519.PrivateKey)
	require.True(t, ok)
	return keyPath, privateKey, stdout.String()
}

// trustOnly makes the helper's notion of "compiled-in keys" exactly pubs.
func trustOnly(t *testing.T, pubs ...ed25519.PublicKey) {
	t.Helper()
	previous := trustedKeys
	trustedKeys = func() ([]updatesig.PublicKey, error) {
		var keys []updatesig.PublicKey
		for _, pub := range pubs {
			keys = append(keys, updatesig.PublicKey{ID: updatesig.KeyID(pub), Key: pub})
		}
		return keys, nil
	}
	t.Cleanup(func() { trustedKeys = previous })
}

func writeTestBinary(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

// keygen prints the public key, its ID and the AGENT_UPDATE_PUBLIC_KEYS line,
// and nothing of the private key.
func TestKeygenPrintsOnlyPublicMaterial(t *testing.T) {
	keyPath, privateKey, out := keygenForTest(t)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	encoded := updatesig.EncodePublicKey(publicKey)

	assert.Contains(t, out, "Key ID:     "+updatesig.KeyID(publicKey)+"\n")
	assert.Contains(t, out, "Public key: "+encoded+"\n")
	line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(encoded) + ` # key [0-9a-f]{8}, created \d{4}-\d{2}-\d{2}$`).FindString(out)
	require.NotEmpty(t, line, out)
	keys, err := updatesig.ParsePublicKeys(line)
	require.NoError(t, err, "the printed line must parse as AGENT_UPDATE_PUBLIC_KEYS")
	assert.True(t, keys[0].Key.Equal(publicKey))
	assert.Contains(t, out, "gh secret set AGENT_UPDATE_SIGNING_KEY < "+keyPath)

	assert.NotContains(t, out, "PRIVATE KEY")
	assert.NotContains(t, out, base64.StdEncoding.EncodeToString(privateKey.Seed()))
	assert.NotContains(t, out, base64.StdEncoding.EncodeToString(privateKey))
	pemData, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	block, _ := pem.Decode(pemData)
	assert.NotContains(t, out, base64.StdEncoding.EncodeToString(block.Bytes))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestKeygenNeverOverwritesAKey(t *testing.T) {
	keyPath, _, _ := keygenForTest(t)
	before, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	var stdout bytes.Buffer
	err = run([]string{"keygen", "-out", keyPath}, &stdout)
	require.Error(t, err)

	after, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Empty(t, stdout.String())
}

// sign with the key from AGENT_UPDATE_SIGNING_KEY (how CI runs it) produces
// .sig files the agent's own verification accepts for exactly the path each
// binary is published at, and verify agrees.
func TestSignRoundTrip(t *testing.T) {
	keyPath, privateKey, _ := keygenForTest(t)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keys := []updatesig.PublicKey{{ID: updatesig.KeyID(publicKey), Key: publicKey}}
	trustOnly(t, publicKey)
	pemData, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	t.Setenv(signingKeyEnv, string(pemData))

	linux := writeTestBinary(t, "reagent-linux-armv7", "linux build")
	windows := writeTestBinary(t, "reagent-windows-amd64", "authenticode-signed exe")
	frpc := writeTestBinary(t, "frpc-windows-amd64", "authenticode-signed frpc")

	var stdout bytes.Buffer
	require.NoError(t, run([]string{"sign", "-version", "1.2.6", linux, windows}, &stdout))
	require.NoError(t, run([]string{"sign", "--version", "0.71.0", frpc}, &stdout))
	assert.NotContains(t, stdout.String(), "PRIVATE KEY")
	assert.Contains(t, stdout.String(), "as reagent-update:v1 linux/armv7/1.2.6/reagent with key "+updatesig.KeyID(publicKey))

	for binary, target := range map[string]updatesig.Target{
		linux:   {Context: updatesig.ContextAgentUpdate, OS: "linux", Arch: "armv7", Version: "1.2.6", FileName: "reagent"},
		windows: {Context: updatesig.ContextAgentUpdate, OS: "windows", Arch: "amd64", Version: "1.2.6", FileName: "reagent.exe"},
		frpc:    {Context: updatesig.ContextFrpcDownload, OS: "windows", Arch: "amd64", Version: "0.71.0", FileName: "frpc.exe"},
	} {
		signature, err := os.ReadFile(binary + ".sig")
		require.NoError(t, err)
		keyID, err := updatesig.VerifyFile(keys, target, binary, signature)
		require.NoError(t, err, target.String())
		assert.Equal(t, updatesig.KeyID(publicKey), keyID)

		// The same signature names nothing else.
		otherVersion := target
		otherVersion.Version = "1.0.0"
		_, err = updatesig.VerifyFile(keys, otherVersion, binary, signature)
		assert.ErrorIs(t, err, updatesig.ErrBadSignature)
	}

	stdout.Reset()
	require.NoError(t, run([]string{"verify", "-version", "1.2.6", linux, windows}, &stdout))
	assert.Contains(t, stdout.String(), "signature OK as reagent-update:v1 windows/amd64/1.2.6/reagent.exe (key "+updatesig.KeyID(publicKey)+")")

	// verify against an explicit public key, as a mirror operator would.
	stdout.Reset()
	require.NoError(t, run([]string{"verify", "-version", "0.71.0", "-public-key", updatesig.EncodePublicKey(publicKey), frpc}, &stdout))

	// verify for another version, or after the binary changed, fails.
	assert.ErrorIs(t, run([]string{"verify", "-version", "1.2.5", linux}, &stdout), updatesig.ErrBadSignature)
	require.NoError(t, os.WriteFile(linux, []byte("linux build, patched"), 0o755))
	assert.ErrorIs(t, run([]string{"verify", "-version", "1.2.6", linux}, &stdout), updatesig.ErrBadSignature)
}

// -kind/-os/-arch name the target when the file name does not.
func TestSignWithExplicitTarget(t *testing.T) {
	keyPath, privateKey, _ := keygenForTest(t)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trustOnly(t, publicKey)
	t.Setenv(signingKeyEnv, "")

	binary := writeTestBinary(t, "downloaded.bin", "linux build")
	var stdout bytes.Buffer
	require.NoError(t, run([]string{"sign", "-key-file", keyPath, "-kind", "reagent", "-os", "linux", "-arch", "arm64", "-version", "2.0.0", binary}, &stdout))

	signature, err := os.ReadFile(binary + ".sig")
	require.NoError(t, err)
	target := updatesig.Target{Context: updatesig.ContextAgentUpdate, OS: "linux", Arch: "arm64", Version: "2.0.0", FileName: "reagent"}
	_, err = updatesig.VerifyFile([]updatesig.PublicKey{{ID: updatesig.KeyID(publicKey), Key: publicKey}}, target, binary, signature)
	require.NoError(t, err)

	// The flags override what the name says.
	named := writeTestBinary(t, "reagent-linux-amd64", "linux build")
	require.NoError(t, run([]string{"sign", "-key-file", keyPath, "-arch", "arm64", "-version", "2.0.0", named}, &stdout))
	signature, err = os.ReadFile(named + ".sig")
	require.NoError(t, err)
	_, err = updatesig.VerifyFile([]updatesig.PublicKey{{ID: updatesig.KeyID(publicKey), Key: publicKey}}, target, named, signature)
	require.NoError(t, err)
}

func TestSignWithKeyFile(t *testing.T) {
	keyPath, privateKey, _ := keygenForTest(t)
	trustOnly(t, privateKey.Public().(ed25519.PublicKey))
	t.Setenv(signingKeyEnv, "")

	binary := writeTestBinary(t, "reagent-darwin-arm64", "darwin build")
	var stdout bytes.Buffer
	require.NoError(t, run([]string{"sign", "-key-file", keyPath, "-version", "1.2.6", binary}, &stdout))
	assert.FileExists(t, binary+".sig")
}

func TestSignRefuses(t *testing.T) {
	keyPath, privateKey, _ := keygenForTest(t)
	pemData, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	t.Run("without a key", func(t *testing.T) {
		trustOnly(t, privateKey.Public().(ed25519.PublicKey))
		t.Setenv(signingKeyEnv, "")
		binary := writeTestBinary(t, "reagent-linux-amd64", "build")

		err := run([]string{"sign", "-version", "1.2.6", binary}, &bytes.Buffer{})
		assert.ErrorContains(t, err, signingKeyEnv+" is empty")
		assert.NoFileExists(t, binary+".sig")
	})

	t.Run("a key the build does not trust", func(t *testing.T) {
		_, otherPrivateKey, _ := keygenForTest(t)
		trustOnly(t, otherPrivateKey.Public().(ed25519.PublicKey))
		t.Setenv(signingKeyEnv, string(pemData))
		binary := writeTestBinary(t, "reagent-linux-amd64", "build")

		err := run([]string{"sign", "-version", "1.2.6", binary}, &bytes.Buffer{})
		assert.ErrorContains(t, err, "is not in AGENT_UPDATE_PUBLIC_KEYS")
		assert.NoFileExists(t, binary+".sig")
	})

	t.Run("a build that still holds the placeholder", func(t *testing.T) {
		previous := trustedKeys
		trustedKeys = func() ([]updatesig.PublicKey, error) { return nil, updatesig.ErrPlaceholderKey }
		t.Cleanup(func() { trustedKeys = previous })
		t.Setenv(signingKeyEnv, string(pemData))
		binary := writeTestBinary(t, "reagent-linux-amd64", "build")

		err := run([]string{"sign", "-version", "1.2.6", binary}, &bytes.Buffer{})
		assert.ErrorIs(t, err, updatesig.ErrPlaceholderKey)
		assert.NoFileExists(t, binary+".sig")
	})

	t.Run("a secret that is not a PEM key, without echoing it", func(t *testing.T) {
		trustOnly(t, privateKey.Public().(ed25519.PublicKey))
		t.Setenv(signingKeyEnv, "c2VjcmV0LWJ1dC1ub3QtcGVt")
		binary := writeTestBinary(t, "reagent-linux-amd64", "build")

		err := run([]string{"sign", "-version", "1.2.6", binary}, &bytes.Buffer{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "c2VjcmV0LWJ1dC1ub3QtcGVt")
	})

	t.Run("without a version", func(t *testing.T) {
		trustOnly(t, privateKey.Public().(ed25519.PublicKey))
		t.Setenv(signingKeyEnv, string(pemData))
		binary := writeTestBinary(t, "reagent-linux-amd64", "build")

		err := run([]string{"sign", binary}, &bytes.Buffer{})
		assert.ErrorContains(t, err, "need -version")
		assert.NoFileExists(t, binary+".sig")
	})

	t.Run("a name that says neither kind nor platform, and no flags", func(t *testing.T) {
		trustOnly(t, privateKey.Public().(ed25519.PublicKey))
		t.Setenv(signingKeyEnv, string(pemData))
		named := writeTestBinary(t, "reagent-linux-amd64", "build")
		odd := writeTestBinary(t, "agent.bin", "build")

		err := run([]string{"sign", "-version", "1.2.6", named, odd}, &bytes.Buffer{})
		assert.ErrorContains(t, err, "neither reagent nor frpc")
		assert.NoFileExists(t, named+".sig", "nothing is signed when one target is unknown")

		err = run([]string{"sign", "-version", "1.2.6", "-kind", "reagent", odd}, &bytes.Buffer{})
		assert.ErrorContains(t, err, "-os/-arch")
		assert.NoFileExists(t, odd+".sig")
	})
}
