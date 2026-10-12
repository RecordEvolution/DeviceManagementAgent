// Package updatesig authenticates the binaries the agent downloads to run
// (its own OTA updates and, on Windows, frpc) with detached Ed25519
// signatures, verified offline against public keys compiled into the agent.
//
// The .sha256 sidecar the agent also checks is fetched from the same origin
// as the binary, so whoever controls that origin (the bucket, the cloud
// download proxy, an appliance's agent mirror) controls both. The signature is
// made with a private key that never leaves the release pipeline, so a
// distribution server can withhold a binary but cannot forge one.
//
// Signature file (<binary>.sig, published next to <binary> and
// <binary>.sha256, e.g. re-agent/linux/arm64/1.2.3/reagent.sig or
// re-agent/frpc/windows/amd64/0.71.0/frpc.exe.sig):
//
//	base64 (standard alphabet, padded) of the raw 64-byte Ed25519 signature
//	over the signed message below, plus an optional newline.
//
// Signed message (ASCII, fields joined by "\n", no trailing newline):
//
//	<context>
//	<os>/<arch>/<version>/<filename>
//	<sha256-hex>
//
//	context   ContextAgentUpdate ("reagent-update:v1") or
//	          ContextFrpcDownload ("frpc-download:v1")
//	os        GOOS: linux, windows, darwin
//	arch      the arch segment of the download path: GOARCH, with the ARM
//	          variant appended (amd64, arm64, armv7, armv6, armv5)
//	version   the version segment of the download path, no "v" (1.2.3)
//	filename  the published file name (reagent, reagent.exe, frpc, frpc.exe)
//	sha256    the binary's SHA-256 digest, 64 lowercase hex characters
//
// e.g. "reagent-update:v1\nlinux/armv7/1.2.3/reagent\n<64 hex>". The middle
// line is the download path below the bucket (re-agent/ or re-agent/frpc/).
// The agent builds the message from what it is about to install (the version
// it resolved, its own platform, the file name), never from the server's
// answer, so a server cannot pass off a signed binary as another version
// (downgrade), another platform, or the other kind of binary.
//
// The bytes signed are exactly the bytes published: on Windows that is the
// Authenticode-signed .exe, so the release signs after Authenticode.
package updatesig

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// AGENT_UPDATE_PUBLIC_KEYS lists the public keys agent updates and the frpc
// downloads may be signed with: one per line, the standard base64 of the raw
// 32-byte Ed25519 key, "#" starts a comment. It is a list so a rotation is an
// edit: ship a release that carries both the old and the new key (still signed
// with the old one), switch the signing secret, and drop the old key in a
// later release. A running agent trusts only the keys compiled into ITSELF, so
// the new key has to reach the fleet before the first release signed with it.
//
// Until the TODO placeholder is replaced, this build trusts no key and refuses
// every update and frpc download, and TestProductionUpdateKeysConfigured
// fails, so CI does not ship it.
//
// Generating the production key (from src/, on a trusted machine):
//
//	go run ./cmd/sign-update keygen -out ~/agent-update-signing.pem
//
// keygen writes the PKCS#8 PEM private key to that file (mode 0600, never
// overwritten) and prints only the public key, its key ID and the line to put
// here. Store the PEM as the GitHub Actions secret AGENT_UPDATE_SIGNING_KEY of
// this repository, e.g.
//
//	gh secret set AGENT_UPDATE_SIGNING_KEY < ~/agent-update-signing.pem
//
// keep an offline backup, and delete the file from the machine.
const AGENT_UPDATE_PUBLIC_KEYS = `
TODO-AGENT-UPDATE-PUBLIC-KEY
`

// publicKeyPlaceholder marks AGENT_UPDATE_PUBLIC_KEYS as not yet configured.
const publicKeyPlaceholder = "TODO-AGENT-UPDATE-PUBLIC-KEY"

// AGENT_UPDATE_ALLOW_UNSIGNED is the escape hatch for a broken signing key
// (lost, or published releases signed with a key the fleet does not trust):
// set to "true" in the agent's service environment (the systemd drop-in or
// Windows service environment that carries the proxy variables), an agent
// update or frpc download whose signature is missing or does not verify is
// installed anyway, with a warning each time. Unset it as soon as the fleet runs a release that trusts
// the current key. A value that is not a boolean keeps verification on.
const AGENT_UPDATE_ALLOW_UNSIGNED = "AGENT_UPDATE_ALLOW_UNSIGNED"

// Contexts: the first line of the signed message, naming what kind of binary
// a signature is for. Bump the version suffix if the message format changes.
const (
	ContextAgentUpdate  = "reagent-update:v1"
	ContextFrpcDownload = "frpc-download:v1"
)

var (
	// ErrPlaceholderKey: AGENT_UPDATE_PUBLIC_KEYS still holds the TODO placeholder.
	ErrPlaceholderKey = errors.New("AGENT_UPDATE_PUBLIC_KEYS still holds the placeholder; no production update key is configured")
	// ErrNoTrustedKeys: there is no key to verify against.
	ErrNoTrustedKeys = errors.New("no trusted update signing key is compiled into this agent")
	// ErrMissingSignature: no .sig is published next to the binary.
	ErrMissingSignature = errors.New("update signature is missing")
	// ErrMalformedSignature: the .sig is not a base64 Ed25519 signature.
	ErrMalformedSignature = errors.New("update signature is malformed")
	// ErrBadSignature: well-formed, but no trusted key signed this binary
	// for this target.
	ErrBadSignature = errors.New("update signature does not verify against any trusted key")
	// ErrInvalidTarget: a target field is empty or would make the signed
	// message ambiguous.
	ErrInvalidTarget = errors.New("invalid signature target")
)

// maxSignatureFileSize bounds what is read from a .sig: 88 base64 characters
// and a line ending, with room for a CRLF or trailing spaces.
const maxSignatureFileSize = 256

// PublicKey is one trusted update signing key.
type PublicKey struct {
	// ID is the first 8 hex characters of the SHA-256 of the raw key. It names
	// the key in logs; verification never relies on it.
	ID  string
	Key ed25519.PublicKey
}

// KeyID returns the ID of a raw Ed25519 public key.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:8]
}

// EncodePublicKey returns the form AGENT_UPDATE_PUBLIC_KEYS lists a key in.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// ParsePublicKeys parses a key list in the AGENT_UPDATE_PUBLIC_KEYS format.
// It is all or nothing: a single unparsable line (the placeholder included)
// rejects the whole list, so a typo can never quietly trust fewer keys.
func ParsePublicKeys(list string) ([]PublicKey, error) {
	var keys []PublicKey
	scanner := bufio.NewScanner(strings.NewReader(list))
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if comment := strings.Index(text, "#"); comment >= 0 {
			text = text[:comment]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if text == publicKeyPlaceholder {
			return nil, ErrPlaceholderKey
		}
		raw, err := base64.StdEncoding.DecodeString(text)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("line %d is not a base64 %d-byte Ed25519 public key", line, ed25519.PublicKeySize)
		}
		pub := ed25519.PublicKey(raw)
		keys = append(keys, PublicKey{ID: KeyID(pub), Key: pub})
	}
	if len(keys) == 0 {
		return nil, ErrNoTrustedKeys
	}
	return keys, nil
}

var (
	trustedOnce sync.Once
	trustedKeys []PublicKey
	trustedErr  error
)

// TrustedKeys returns the keys compiled in through AGENT_UPDATE_PUBLIC_KEYS,
// or why there are none.
func TrustedKeys() ([]PublicKey, error) {
	trustedOnce.Do(func() {
		trustedKeys, trustedErr = ParsePublicKeys(AGENT_UPDATE_PUBLIC_KEYS)
	})
	return trustedKeys, trustedErr
}

// Target names what a signature is for: the kind of binary and the download
// path it is installed from.
type Target struct {
	Context  string // ContextAgentUpdate or ContextFrpcDownload
	OS       string
	Arch     string
	Version  string
	FileName string
}

// Path is the target's download path below the bucket.
func (t Target) Path() string {
	return t.OS + "/" + t.Arch + "/" + t.Version + "/" + t.FileName
}

func (t Target) String() string {
	return t.Context + " " + t.Path()
}

// Message returns the bytes signed for a binary with this SHA-256 digest.
func (t Target) Message(digest []byte) ([]byte, error) {
	for _, field := range []string{t.Context, t.OS, t.Arch, t.Version, t.FileName} {
		if field == "" || strings.ContainsAny(field, "/\n\r") {
			return nil, fmt.Errorf("%w: %q", ErrInvalidTarget, t.String())
		}
	}
	if len(digest) != sha256.Size {
		return nil, fmt.Errorf("%w: digest of %d bytes", ErrInvalidTarget, len(digest))
	}
	return []byte(t.Context + "\n" + t.Path() + "\n" + hex.EncodeToString(digest)), nil
}

// FileDigest returns the SHA-256 digest of the file at path.
func FileDigest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hasher := sha256.New()
	_, err = io.Copy(hasher, file)
	if err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}

// EncodeSignature returns the .sig file contents for a raw signature.
func EncodeSignature(signature []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(signature) + "\n")
}

// ParseSignature decodes .sig file contents into the raw 64-byte signature.
func ParseSignature(data []byte) ([]byte, error) {
	if len(data) > maxSignatureFileSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrMalformedSignature, len(data))
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedSignature, err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: %d bytes instead of %d", ErrMalformedSignature, len(signature), ed25519.SignatureSize)
	}
	return signature, nil
}

// fileMessage returns the signed message for the file at path as target.
func fileMessage(target Target, path string) ([]byte, error) {
	digest, err := FileDigest(path)
	if err != nil {
		return nil, err
	}
	return target.Message(digest)
}

// SignFile returns the .sig file contents for the binary at path, published
// as target.
func SignFile(privateKey ed25519.PrivateKey, target Target, path string) ([]byte, error) {
	message, err := fileMessage(target, path)
	if err != nil {
		return nil, err
	}
	return EncodeSignature(ed25519.Sign(privateKey, message)), nil
}

// VerifyFile checks the .sig contents against the binary at path, installed
// as target, and returns the ID of the key that signed it. Any of keys may
// have: that is what lets a rotation overlap.
func VerifyFile(keys []PublicKey, target Target, path string, signatureFile []byte) (string, error) {
	if len(keys) == 0 {
		return "", ErrNoTrustedKeys
	}
	signature, err := ParseSignature(signatureFile)
	if err != nil {
		return "", err
	}
	message, err := fileMessage(target, path)
	if err != nil {
		return "", err
	}
	for _, key := range keys {
		if ed25519.Verify(key.Key, message, signature) {
			return key.ID, nil
		}
	}
	return "", fmt.Errorf("%w as %s", ErrBadSignature, target)
}

// AllowUnsigned reports whether AGENT_UPDATE_ALLOW_UNSIGNED asks to install
// updates whose signature is missing or invalid. A value that is not a
// boolean keeps verification on rather than guessing.
func AllowUnsigned() bool {
	value := strings.TrimSpace(os.Getenv(AGENT_UPDATE_ALLOW_UNSIGNED))
	if value == "" {
		return false
	}
	allow, err := strconv.ParseBool(value)
	if err != nil {
		log.Warn().Str("value", value).
			Msgf("Ignoring %s: not a boolean, update signature verification stays on", AGENT_UPDATE_ALLOW_UNSIGNED)
		return false
	}
	return allow
}
