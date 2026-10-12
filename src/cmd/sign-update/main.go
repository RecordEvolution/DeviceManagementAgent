// Command sign-update creates the Ed25519 signatures the agent requires on an
// OTA update and on the frpc it downloads (format: package updatesig), and the
// key that makes them. Run it from src/:
//
//	go run ./cmd/sign-update keygen -out <private-key.pem>
//	go run ./cmd/sign-update sign -version <v> [target flags] [-key-file <pem>] <binary>...
//	go run ./cmd/sign-update verify -version <v> [target flags] [-public-key <base64>] <binary>...
//
// keygen writes a new PKCS#8 PEM private key to the chosen file (mode 0600,
// never overwriting one) and prints only the public key, its key ID and the
// line for updatesig.AGENT_UPDATE_PUBLIC_KEYS.
//
// A signature names what it is for (updatesig.Target): the kind of binary and
// the os/arch/version/file it is published as. -version is always required.
// Kind, os and arch come from a build output name <kind>-<os>-<arch>
// (build/reagent-linux-armv7, build/frpc-windows-amd64; kind is reagent or
// frpc) unless -kind, -os and -arch say otherwise. The file name follows: kind,
// plus .exe on windows.
//
// sign writes <binary>.sig next to each binary. The private key is the PEM in
// AGENT_UPDATE_SIGNING_KEY, which is how release CI hands over its secret,
// unless -key-file names a PEM file. A key that is not in this build's
// AGENT_UPDATE_PUBLIC_KEYS is refused: agents built from this tree would reject
// what it signs.
//
// verify checks <binary>.sig for the same target against the compiled-in
// keys, or against the one given with -public-key.
//
// No subcommand ever prints private key material.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reagent/updatesig"
	"strings"
	"time"
)

// signingKeyEnv names the variable release CI passes the private key in.
const signingKeyEnv = "AGENT_UPDATE_SIGNING_KEY"

const usage = `usage:
  sign-update keygen -out <private-key.pem>
  sign-update sign -version <v> [-kind reagent|frpc] [-os <os>] [-arch <arch>] [-key-file <pem>] <binary>...
  sign-update verify -version <v> [-kind reagent|frpc] [-os <os>] [-arch <arch>] [-public-key <base64>] <binary>...`

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
	err := run(os.Args[1:], os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign-update: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:], stdout)
	case "sign":
		return sign(args[1:], stdout)
	case "verify":
		return verify(args[1:], stdout)
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
	fmt.Fprintf(stdout, "Private key written to %s (mode 0600).\n", *out)
	fmt.Fprintf(stdout, "Key ID:     %s\n", keyID)
	fmt.Fprintf(stdout, "Public key: %s\n\n", encoded)
	fmt.Fprintf(stdout, "Add this line to AGENT_UPDATE_PUBLIC_KEYS in src/updatesig/updatesig.go:\n\n")
	fmt.Fprintf(stdout, "%s # key %s, created %s\n\n", encoded, keyID, time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(stdout, "Then store the private key as the GitHub Actions secret %s, keep an\n", signingKeyEnv)
	fmt.Fprintf(stdout, "offline backup, and delete the file:\n\n")
	fmt.Fprintf(stdout, "  gh secret set %s < %s\n", signingKeyEnv, *out)
	return nil
}

func sign(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyFile := flags.String("key-file", "", "PKCS#8 PEM private key file (default: the PEM in $"+signingKeyEnv+")")
	targets := addTargetFlags(flags)
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return errors.New("sign needs at least one binary")
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

	var keys []updatesig.PublicKey
	if *publicKey != "" {
		keys, err = updatesig.ParsePublicKeys(*publicKey)
	} else {
		keys, err = trustedKeys()
	}
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

// loadPrivateKey reads the PKCS#8 PEM key from keyFile, or from the signing
// key variable when keyFile is empty. Errors never quote the key material.
func loadPrivateKey(keyFile string) (ed25519.PrivateKey, error) {
	var data []byte
	source := "$" + signingKeyEnv
	if keyFile != "" {
		var err error
		data, err = os.ReadFile(keyFile)
		if err != nil {
			return nil, err
		}
		source = keyFile
	} else {
		data = []byte(strings.TrimSpace(os.Getenv(signingKeyEnv)))
		if len(data) == 0 {
			return nil, fmt.Errorf("%s is empty; pass the PKCS#8 PEM private key in it or use -key-file", signingKeyEnv)
		}
	}

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
