package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The published SHA-256 fingerprints of the Let's Encrypt roots
// (https://letsencrypt.org/certificates/). A change to the embedded file that
// does not also change these is a tampered or mistaken root.
var isrgRootFingerprints = map[string]string{
	"ISRG Root X1": "96bcec06264976f37460779acf28c5a7cfe8a3c0aae11a8ffcee05c0bddf08c6",
	"ISRG Root X2": "69729b8e15a86efc177a57afb7171dfc64add28c2fca8cf1507e34453ccb1470",
	"Root YR":      "e57b7e6f150c419102e8d5c055729ff967b9d1a829bf00cec89ca604ebf4a86f",
	"Root YE":      "e14ffcad5b0025731006caa43a121a22d8e9700f4fb9cf852f02a708aa5d5666",
}

var embeddedRootNames = []string{"ISRG Root X1", "ISRG Root X2", "Root YR", "Root YE"}

func TestEmbeddedTunnelRootsAreTheISRGRoots(t *testing.T) {
	ders := decodePEMCertificates(letsEncryptRootsPEM)
	require.Len(t, ders, len(isrgRootFingerprints))

	names := commonNames(t, buildPEM(ders))
	require.Len(t, names, len(isrgRootFingerprints), "both roots must survive buildPEM (CA, unexpired)")

	for _, der := range ders {
		sum := sha256.Sum256(der)
		fingerprint := hex.EncodeToString(sum[:])

		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		assert.Equal(t, isrgRootFingerprints[cert.Subject.CommonName], fingerprint,
			"embedded %q does not match its published SHA-256", cert.Subject.CommonName)
	}
}

// The bundle frpc verifies against REPLACES the system roots, so it must hold
// the host store (the corporate root of a domain-mode appliance), the device
// endpoint's chain AND the embedded Let's Encrypt roots (the cloud server on a
// minimal system).
func TestWriteTunnelCAIncludesHostEndpointAndEmbeddedRoots(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)
	corporateRoot := testCert(t, "CA-TRUMPF-SUB-01", true, future)

	fixtures := t.TempDir()
	hostBundle := filepath.Join(fixtures, "ca-certificates.crt")
	require.NoError(t, os.WriteFile(hostBundle, buildPEM([][]byte{corporateRoot}), 0o644))

	// The first candidate is absent, as on any distribution but one.
	hostRoots, err := hostRootsFromFiles([]string{filepath.Join(fixtures, "missing.crt"), hostBundle}, nil)
	require.NoError(t, err)

	// What the WAMP connection's verified chain contributes: the issuing
	// intermediate and root of the appliance ingress.
	endpointChain := [][]byte{testCert(t, "Corp Issuing CA", true, future)}

	agentDir := t.TempDir()
	path, err := WriteTunnelCA(agentDir, hostRoots, endpointChain)
	require.NoError(t, err)
	assert.Equal(t, TunnelCAPath(agentDir), path)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	names := commonNames(t, written)
	assert.Contains(t, names, "CA-TRUMPF-SUB-01", "the host store must be carried over")
	assert.Contains(t, names, "Corp Issuing CA", "the endpoint chain must be carried over")
	for _, name := range embeddedRootNames {
		assert.Contains(t, names, name)
	}

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	}
}

// No readable host store must still yield a bundle the cloud server verifies
// against, rather than no bundle (frpc would refuse to start TLS) or none at
// all (frpc would go back to verifying nothing).
func TestWriteTunnelCAFallsBackToEmbeddedRoots(t *testing.T) {
	fixtures := t.TempDir()
	hostRoots, err := hostRootsFromFiles([]string{filepath.Join(fixtures, "missing.crt")}, []string{filepath.Join(fixtures, "missing")})
	require.ErrorIs(t, err, errNoHostStore)

	path, err := WriteTunnelCA(t.TempDir(), hostRoots, nil)
	require.NoError(t, err)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.ElementsMatch(t, embeddedRootNames, commonNames(t, written))
}

// Go's system pool reads the first bundle AND every file in the certificate
// directories, so a root an administrator copied to /etc/ssl/certs/corp.pem
// without running update-ca-certificates is trusted by the WAMP connection.
// The tunnel must trust it too.
func TestHostRootsFromFilesReadsBundleAndWholeDirectoryLikeGo(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)
	certsDir := filepath.Join(t.TempDir(), "certs")
	require.NoError(t, os.MkdirAll(certsDir, 0o755))

	bundle := filepath.Join(certsDir, "ca-certificates.crt")
	require.NoError(t, os.WriteFile(bundle, buildPEM([][]byte{testCert(t, "Public Root", true, future)}), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(certsDir, "corp.pem"), buildPEM([][]byte{testCert(t, "Corp Root", true, future)}), 0o644))
	// No extension filter: Go reads every file and so must we.
	require.NoError(t, os.WriteFile(filepath.Join(certsDir, "legacy-root"), buildPEM([][]byte{testCert(t, "Legacy Root", true, future)}), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(certsDir, "README"), []byte("not a certificate"), 0o644))

	ders, err := hostRootsFromFiles([]string{filepath.Join(t.TempDir(), "missing.crt"), bundle}, []string{certsDir})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Public Root", "Corp Root", "Legacy Root"}, commonNames(t, buildPEM(ders)))
}

func TestHostRootsFromFilesWithoutBundleUsesDirectory(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corp-root.pem"), buildPEM([][]byte{testCert(t, "Corp Root", true, future)}), 0o644))

	ders, err := hostRootsFromFiles([]string{filepath.Join(dir, "missing.crt")}, []string{dir})
	require.NoError(t, err)
	assert.Equal(t, []string{"Corp Root"}, commonNames(t, buildPEM(ders)))
}

// Go accepts a self-signed certificate as a root whether or not it is marked
// as a CA (a v1 corporate root, a server certificate trusted explicitly). The
// bundle must keep it; a leaf someone else issued is still no anchor.
func TestBuildPEMKeepsSelfSignedAnchorsNotMarkedCA(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)
	selfSigned := testCert(t, "appliance.local", false, future)
	issuedLeaf := testIssuedLeaf(t, "registry.example.com")

	names := commonNames(t, buildPEM([][]byte{selfSigned, issuedLeaf}))
	assert.Equal(t, []string{"appliance.local"}, names)
}

// testIssuedLeaf mints a leaf certificate signed by a throwaway CA, i.e. one
// that is neither a CA nor self-signed.
func testIssuedLeaf(t *testing.T, commonName string) []byte {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "Issuing CA for " + commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	return der
}
