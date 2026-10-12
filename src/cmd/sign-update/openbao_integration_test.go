//go:build integration

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reagent/updatesig"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openBaoImage is the image production runs (REDeployments values.yaml).
const openBaoImage = "quay.io/openbao/openbao:2.7.1"

// devOpenBao is a throwaway OpenBao dev server in docker: in-memory, unsealed,
// root token "dev-root". Nothing here talks to a real OpenBao.
type devOpenBao struct {
	t    *testing.T
	name string
}

func startDevOpenBao(t *testing.T) *devOpenBao {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if err := exec.Command("docker", "image", "inspect", openBaoImage).Run(); err != nil {
		t.Skipf("image %s not present; `docker pull %s` to run this test", openBaoImage, openBaoImage)
	}

	bao := &devOpenBao{t: t, name: fmt.Sprintf("sign-update-openbao-%d-%d", os.Getpid(), time.Now().UnixNano())}
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", bao.name,
		"-e", "BAO_DEV_ROOT_TOKEN_ID=dev-root", openBaoImage,
		"server", "-dev", "-dev-listen-address=0.0.0.0:8200").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "stop", bao.name).Run() })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := bao.try(nil, "bao", "status"); err == nil {
			return bao
		}
		if time.Now().After(deadline) {
			t.Fatalf("OpenBao dev server %s did not come up", bao.name)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (b *devOpenBao) try(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"exec", "-i",
		"-e", "BAO_ADDR=http://127.0.0.1:8200", "-e", "BAO_TOKEN=dev-root", b.name}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%v: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func (b *devOpenBao) run(stdin []byte, args ...string) string {
	b.t.Helper()
	out, err := b.try(stdin, args...)
	require.NoError(b.t, err)
	return out
}

// signBatch runs scripts/openbao-transit-sign.sh, the text sign-release.sh
// sends into the production pod, against the dev server.
func (b *devOpenBao) signBatch(requests string, keyVersion string) map[string]string {
	b.t.Helper()
	script, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "openbao-transit-sign.sh"))
	require.NoError(b.t, err)
	out := b.run([]byte(requests), "sh", "-c", string(script), "sh", "agent-update", keyVersion)

	answers := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id, signature, ok := strings.Cut(line, " ")
		require.True(b.t, ok, line)
		answers[id] = signature
	}
	return answers
}

// A real OpenBao transit ed25519 key signs the message `sign-update message`
// builds; sig-from-openbao turns the answer into a .sig; the agent's verifier
// accepts it for exactly that target, with the key pubkey-from-openbao
// printed. A rotation keeps the old version usable through key_version.
func TestOpenBaoTransitSignsWhatTheAgentVerifies(t *testing.T) {
	bao := startDevOpenBao(t)
	bao.run(nil, "bao", "secrets", "enable", "transit")
	// The README's one-time setup, verbatim.
	bao.run(nil, "bao", "write", "-f", "transit/keys/agent-update", "type=ed25519", "exportable=false", "allow_plaintext_backup=false")
	_, err := bao.try(nil, "bao", "read", "transit/export/signing-key/agent-update")
	require.Error(t, err, "the key must not be exportable")

	pubkeys := func() []updatesig.PublicKey {
		var lines bytes.Buffer
		require.NoError(t, run([]string{"pubkey-from-openbao"}, strings.NewReader(bao.run(nil, "bao", "read", "-format=json", "transit/keys/agent-update")), &lines))
		keys, err := updatesig.ParsePublicKeys(lines.String())
		require.NoError(t, err, lines.String())
		return keys
	}
	keys := pubkeys()
	require.Len(t, keys, 1)
	trustOnly(t, keys[0].Key)

	agent := writeTestBinary(t, "reagent-linux-armv7", "agent build")
	frpc := writeTestBinary(t, "frpc-windows-amd64", "frpc build")
	agentTarget := updatesig.Target{Context: updatesig.ContextAgentUpdate, OS: "linux", Arch: "armv7", Version: "1.2.6", FileName: "reagent"}
	frpcTarget := updatesig.Target{Context: updatesig.ContextFrpcDownload, OS: "windows", Arch: "amd64", Version: "0.71.0", FileName: "frpc.exe"}

	message := func(binary, version string) string {
		var out bytes.Buffer
		require.NoError(t, run([]string{"message", "-version", version, binary}, nil, &out))
		return strings.TrimSpace(out.String())
	}
	requests := "0 " + message(agent, "1.2.6") + "\n1 " + message(frpc, "0.71.0") + "\n"

	answers := bao.signBatch(requests, "")
	require.Len(t, answers, 2)
	for id, binary := range map[string]string{"0": agent, "1": frpc} {
		version := "1.2.6"
		if binary == frpc {
			version = "0.71.0"
		}
		assert.True(t, strings.HasPrefix(answers[id], "vault:v1:"), answers[id])
		var out bytes.Buffer
		require.NoError(t, run([]string{"sig-from-openbao", "-version", version, binary}, strings.NewReader(answers[id]), &out))
		t.Log(strings.TrimSpace(out.String()))
	}

	for binary, target := range map[string]updatesig.Target{agent: agentTarget, frpc: frpcTarget} {
		signature, err := os.ReadFile(binary + ".sig")
		require.NoError(t, err)
		keyID, err := updatesig.VerifyFile(keys, target, binary, signature)
		require.NoError(t, err, target.String())
		assert.Equal(t, keys[0].ID, keyID)

		other := target
		other.Version = "9.9.9"
		_, err = updatesig.VerifyFile(keys, other, binary, signature)
		assert.ErrorIs(t, err, updatesig.ErrBadSignature)
	}

	// Rotation: version 2 exists, signing can stay on version 1 meanwhile.
	bao.run(nil, "bao", "write", "-f", "transit/keys/agent-update/rotate")
	keys = pubkeys()
	require.Len(t, keys, 2)

	pinned := bao.signBatch("0 "+message(agent, "1.2.7")+"\n", "1")
	latest := bao.signBatch("0 "+message(agent, "1.2.7")+"\n", "")
	require.True(t, strings.HasPrefix(pinned["0"], "vault:v1:"), pinned["0"])
	require.True(t, strings.HasPrefix(latest["0"], "vault:v2:"), latest["0"])

	newTarget := agentTarget
	newTarget.Version = "1.2.7"
	for answer, onlyKey := range map[string]updatesig.PublicKey{pinned["0"]: keys[0], latest["0"]: keys[1]} {
		trustOnly(t, onlyKey.Key)
		require.NoError(t, run([]string{"sig-from-openbao", "-version", "1.2.7", agent}, strings.NewReader(answer), &bytes.Buffer{}))
		signature, err := os.ReadFile(agent + ".sig")
		require.NoError(t, err)
		_, err = updatesig.VerifyFile([]updatesig.PublicKey{onlyKey}, newTarget, agent, signature)
		require.NoError(t, err)
		require.NoError(t, os.Remove(agent+".sig"))
	}

	// An agent that only trusts version 1 refuses what version 2 signed.
	trustOnly(t, keys[0].Key)
	assert.Error(t, run([]string{"sig-from-openbao", "-version", "1.2.7", agent}, strings.NewReader(latest["0"]), &bytes.Buffer{}))
	assert.NoFileExists(t, agent+".sig")
}
