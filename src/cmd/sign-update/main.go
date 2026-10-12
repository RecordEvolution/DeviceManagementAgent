// Command sign-update builds, converts and checks the Ed25519 signatures the
// agent requires on an OTA update and on the frpc it downloads (format:
// package updatesig). Run it from src/.
//
// Release signing (the private key never leaves OpenBao; scripts/sign-release.sh
// drives these):
//
//	message -version <v> [target flags] <binary>
//	    prints the base64 of the exact bytes to sign, for transit/sign input=
//	sig-from-openbao -version <v> [target flags] [-public-key <b64>] <binary>
//	    reads OpenBao's "vault:v<N>:<base64>" from stdin and writes <binary>.sig,
//	    but only once it verifies against the compiled-in keys
//	verify -version <v> [target flags] [-public-key <b64>] <binary>...
//	    checks <binary>.sig against the compiled-in keys (or -public-key)
//	pubkey-from-openbao
//	    reads `bao read -format=json transit/keys/agent-update` from stdin and
//	    prints the AGENT_UPDATE_PUBLIC_KEYS line of every key version
//
// Development and tests only, with a local key file that never signs a release:
//
//	keygen -out <private-key.pem>
//	sign -key-file <private-key.pem> -version <v> [target flags] <binary>...
//
// A signature names what it is for (updatesig.Target): the kind of binary and
// the os/arch/version/file it is published as. -version is always required.
// Kind, os and arch come from a build output name <kind>-<os>-<arch>
// (build/reagent-linux-armv7, build/frpc-windows-amd64; kind is reagent or
// frpc) unless -kind, -os and -arch say otherwise. The file name follows: kind,
// plus .exe on windows.
//
// sign refuses a key that is not in this build's AGENT_UPDATE_PUBLIC_KEYS:
// agents built from this tree would reject what it signs. No subcommand ever
// prints private key material.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reagent/updatesig"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usage = `usage (target flags: -version <v> [-kind reagent|frpc] [-os <os>] [-arch <arch>]):
  sign-update message <target flags> <binary>
  sign-update sig-from-openbao <target flags> [-public-key <base64>] <binary>  < vault:v<N>:<base64>
  sign-update verify <target flags> [-public-key <base64>] <binary>...
  sign-update pubkey-from-openbao  < bao read -format=json transit/keys/agent-update
  sign-update keygen -out <private-key.pem>                          (development only)
  sign-update sign -key-file <pem> <target flags> <binary>...        (development only)`

// signatureContexts maps a binary kind to the context its signatures carry.
var signatureContexts = map[string]string{
	"reagent": updatesig.ContextAgentUpdate,
	"frpc":    updatesig.ContextFrpcDownload,
}

// targetFlags are the flags that say what a binary is signed for.
type targetFlags struct {
	version, kind, os, arch *string
}

func addTargetFlags(flags *flag.FlagSet) targetFlags {
	return targetFlags{
		version: flags.String("version", "", "version the binaries are published as (required)"),
		kind:    flags.String("kind", "", "reagent or frpc (default: from the file name)"),
		os:      flags.String("os", "", "GOOS the binaries are for (default: from the file name)"),
		arch:    flags.String("arch", "", "arch path segment, e.g. amd64 or armv7 (default: from the file name)"),
	}
}

// targetFor returns what binary is published as: the flags where given,
// otherwise its build output name <kind>-<os>-<arch>.
func (f targetFlags) targetFor(binary string) (updatesig.Target, error) {
	kind, goos, arch := *f.kind, *f.os, *f.arch
	parts := strings.Split(filepath.Base(binary), "-")
	if kind == "" {
		kind = parts[0]
	}
	if len(parts) == 3 {
		if goos == "" {
			goos = parts[1]
		}
		if arch == "" {
			arch = parts[2]
		}
	}

	context, ok := signatureContexts[kind]
	if !ok {
		return updatesig.Target{}, fmt.Errorf("%s: kind %q is neither reagent nor frpc; pass -kind", binary, kind)
	}
	if *f.version == "" || goos == "" || arch == "" {
		return updatesig.Target{}, fmt.Errorf("%s: need -version, and -os/-arch unless the name is <kind>-<os>-<arch>", binary)
	}
	fileName := kind
	if goos == "windows" {
		fileName += ".exe"
	}
	return updatesig.Target{Context: context, OS: goos, Arch: arch, Version: *f.version, FileName: fileName}, nil
}

// trustedKeys supplies the keys sign accepts and verify checks against. A
// variable so tests can work with a key generated at test time.
var trustedKeys = updatesig.TrustedKeys

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign-update: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "message":
		return message(args[1:], stdout)
	case "sig-from-openbao":
		return sigFromOpenBao(args[1:], stdin, stdout)
	case "verify":
		return verify(args[1:], stdout)
	case "pubkey-from-openbao":
		return pubkeyFromOpenBao(args[1:], stdin, stdout)
	case "keygen":
		return keygen(args[1:], stdout)
	case "sign":
		return sign(args[1:], stdout)
	}
	return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage)
}

func keygen(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := flags.String("out", "", "file to write the PKCS#8 PEM private key to (must not exist)")
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if *out == "" || flags.NArg() != 0 {
		return errors.New("keygen needs -out <private-key.pem> and nothing else")
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}

	// O_EXCL: an existing key file is someone's signing key; never replace it.
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create the private key file: %w", err)
	}
	err = pem.Encode(file, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(*out)
		return fmt.Errorf("cannot write the private key file: %w", err)
	}

	keyID := updatesig.KeyID(publicKey)
	encoded := updatesig.EncodePublicKey(publicKey)
	fmt.Fprintf(stdout, "DEVELOPMENT key written to %s (mode 0600). Releases are signed by OpenBao, never with a key file.\n", *out)
	fmt.Fprintf(stdout, "Key ID:     %s\n", keyID)
	fmt.Fprintf(stdout, "Public key: %s\n\n", encoded)
	fmt.Fprintf(stdout, "To test with it, add this line to AGENT_UPDATE_PUBLIC_KEYS in a local build (never commit it):\n\n")
	fmt.Fprintf(stdout, "%s # DEVELOPMENT key %s, created %s\n", encoded, keyID, time.Now().UTC().Format("2006-01-02"))
	return nil
}

func sign(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyFile := flags.String("key-file", "", "PKCS#8 PEM private key file of a DEVELOPMENT key (required)")
	targets := addTargetFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() == 0 || *keyFile == "" {
		return errors.New("sign needs -key-file <development key> and at least one binary; releases are signed through OpenBao (scripts/sign-release.sh)")
	}

	privateKey, err := loadPrivateKey(*keyFile)
	if err != nil {
		return err
	}

	keys, err := trustedKeys()
	if err != nil {
		return fmt.Errorf("this build trusts no update key, so agents built from it would refuse every update: %w", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := updatesig.KeyID(publicKey)
	trusted := false
	for _, key := range keys {
		if key.Key.Equal(publicKey) {
			trusted = true
		}
	}
	if !trusted {
		return fmt.Errorf("signing key %s is not in AGENT_UPDATE_PUBLIC_KEYS; agents would refuse what it signs", keyID)
	}

	// Resolve every target first, so a bad name signs nothing at all.
	signTargets := make([]updatesig.Target, flags.NArg())
	for i, binary := range flags.Args() {
		signTargets[i], err = targets.targetFor(binary)
		if err != nil {
			return err
		}
	}

	for i, binary := range flags.Args() {
		target := signTargets[i]
		signature, err := updatesig.SignFile(privateKey, target, binary)
		if err != nil {
			return err
		}
		// Read back through the agent's own verification, so a format slip
		// fails here and not on the devices.
		_, err = updatesig.VerifyFile(keys, target, binary, signature)
		if err != nil {
			return fmt.Errorf("%s: fresh signature does not verify: %w", binary, err)
		}
		err = os.WriteFile(binary+".sig", signature, 0o644)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "signed %s as %s with key %s -> %s.sig\n", binary, target, keyID, binary)
	}
	return nil
}

func verify(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	publicKey := flags.String("public-key", "", "base64 Ed25519 public key (default: the keys compiled into this build)")
	targets := addTargetFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return errors.New("verify needs at least one binary")
	}

	keys, err := verificationKeys(*publicKey)
	if err != nil {
		return err
	}

	for _, binary := range flags.Args() {
		target, err := targets.targetFor(binary)
		if err != nil {
			return err
		}
		signature, err := os.ReadFile(binary + ".sig")
		if err != nil {
			return err
		}
		keyID, err := updatesig.VerifyFile(keys, target, binary, signature)
		if err != nil {
			return fmt.Errorf("%s: %w", binary, err)
		}
		fmt.Fprintf(stdout, "%s: signature OK as %s (key %s)\n", binary, target, keyID)
	}
	return nil
}

// verificationKeys returns the key given with -public-key, or else the keys
// compiled into this build.
func verificationKeys(publicKey string) ([]updatesig.PublicKey, error) {
	if publicKey != "" {
		return updatesig.ParsePublicKeys(publicKey)
	}
	return trustedKeys()
}

// message prints the base64 of the exact bytes a signature for binary is made
// over, the input OpenBao's transit/sign takes. The digest is computed here,
// from the file, never taken from a .sha256 sidecar.
func message(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("message", flag.ContinueOnError)
	targets := addTargetFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("message needs exactly one binary")
	}
	binary := flags.Arg(0)

	target, err := targets.targetFor(binary)
	if err != nil {
		return err
	}
	digest, err := updatesig.FileDigest(binary)
	if err != nil {
		return err
	}
	signed, err := target.Message(digest)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(signed))
	return nil
}

// parseTransitSignature splits what OpenBao transit/sign returns,
// "vault:v<key version>:<base64 signature>", into the key version and the raw
// 64-byte Ed25519 signature.
func parseTransitSignature(text string) (int, []byte, error) {
	malformed := errors.New(`not an OpenBao transit Ed25519 signature ("vault:v<N>:<base64 of 64 bytes>")`)
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "vault:v")
	if !ok {
		return 0, nil, malformed
	}
	versionText, encoded, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, nil, malformed
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 || strconv.Itoa(version) != versionText {
		return 0, nil, malformed
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return 0, nil, malformed
	}
	return version, signature, nil
}

// sigFromOpenBao turns OpenBao's signature for binary into <binary>.sig, and
// writes it only if it verifies for the target against the compiled-in keys
// (or -public-key), so a signature the agents would refuse is never produced.
func sigFromOpenBao(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("sig-from-openbao", flag.ContinueOnError)
	publicKey := flags.String("public-key", "", "base64 Ed25519 public key (default: the keys compiled into this build)")
	targets := addTargetFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("sig-from-openbao needs exactly one binary")
	}
	binary := flags.Arg(0)

	target, err := targets.targetFor(binary)
	if err != nil {
		return err
	}
	keys, err := verificationKeys(*publicKey)
	if err != nil {
		return err
	}
	text, err := io.ReadAll(io.LimitReader(stdin, 1024))
	if err != nil {
		return err
	}
	keyVersion, raw, err := parseTransitSignature(string(text))
	if err != nil {
		return fmt.Errorf("%s: %w", binary, err)
	}

	signature := updatesig.EncodeSignature(raw)
	keyID, err := updatesig.VerifyFile(keys, target, binary, signature)
	if err != nil {
		return fmt.Errorf("%s: OpenBao's signature (transit key version %d) does not verify: %w", binary, keyVersion, err)
	}
	err = os.WriteFile(binary+".sig", signature, 0o644)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: signed as %s by transit key version %d (key %s) -> %s.sig\n", binary, target, keyVersion, keyID, binary)
	return nil
}

// transitKey is the part of `bao read -format=json transit/keys/<name>` that
// pubkeyFromOpenBao needs.
type transitKey struct {
	Data struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Keys map[string]struct {
			PublicKey    string `json:"public_key"`
			CreationTime string `json:"creation_time"`
		} `json:"keys"`
	} `json:"data"`
}

// pubkeyFromOpenBao prints the AGENT_UPDATE_PUBLIC_KEYS line of every version
// of an OpenBao transit key, oldest first.
func pubkeyFromOpenBao(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("pubkey-from-openbao takes no arguments; pipe `bao read -format=json transit/keys/agent-update` into it")
	}
	var key transitKey
	err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&key)
	if err != nil {
		return fmt.Errorf("not the JSON of `bao read -format=json transit/keys/<name>`: %w", err)
	}
	if key.Data.Type != "ed25519" {
		return fmt.Errorf("transit key %q is of type %q, not ed25519", key.Data.Name, key.Data.Type)
	}
	if len(key.Data.Keys) == 0 {
		return fmt.Errorf("transit key %q lists no key versions", key.Data.Name)
	}

	versions := make([]int, 0, len(key.Data.Keys))
	for versionText := range key.Data.Keys {
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 1 {
			return fmt.Errorf("transit key %q has a key version %q", key.Data.Name, versionText)
		}
		versions = append(versions, version)
	}
	sort.Ints(versions)

	for _, version := range versions {
		entry := key.Data.Keys[strconv.Itoa(version)]
		raw, err := base64.StdEncoding.DecodeString(entry.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("transit key %q version %d has no base64 32-byte public key", key.Data.Name, version)
		}
		created, _, _ := strings.Cut(entry.CreationTime, "T")
		line := fmt.Sprintf("%s # key %s, OpenBao transit %s v%d", updatesig.EncodePublicKey(raw), updatesig.KeyID(raw), key.Data.Name, version)
		if created != "" {
			line += ", created " + created
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// loadPrivateKey reads a development PKCS#8 PEM key from keyFile. Errors never
// quote the key material.
func loadPrivateKey(keyFile string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	source := keyFile

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s holds no PKCS#8 PEM private key (\"-----BEGIN PRIVATE KEY-----\")", source)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot parse the PKCS#8 private key", source)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s holds a %T key, not an Ed25519 one", source, key)
	}
	return privateKey, nil
}
