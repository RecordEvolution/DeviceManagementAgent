package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var serial int64

// testCert mints a self-signed certificate. isCA and notAfter drive the two
// filters buildPEM applies.
func testCert(t *testing.T, commonName string, isCA bool, notAfter time.Time) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial++
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              notAfter,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return der
}

// commonNames returns the subjects of every certificate in a PEM bundle, which
// is what the assertions below actually care about.
func commonNames(t *testing.T, bundle []byte) []string {
	t.Helper()

	var names []string
	for _, der := range decodePEMCertificates(bundle) {
		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		names = append(names, cert.Subject.CommonName)
	}
	return names
}

func TestBuildPEMKeepsOnlyUsableCAs(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)

	corporateRoot := testCert(t, "CA-TRUMPF-SUB-01", true, future)
	publicRoot := testCert(t, "Public Root", true, future)
	leaf := testCert(t, "registry.example.com", false, future)
	expired := testCert(t, "Expired Root", true, time.Now().Add(-time.Hour))

	bundle := buildPEM([][]byte{publicRoot, leaf, expired}, [][]byte{corporateRoot})

	names := commonNames(t, bundle)
	assert.Contains(t, names, "CA-TRUMPF-SUB-01", "the appliance CA is the whole point of the bundle")
	assert.Contains(t, names, "Public Root", "host roots must survive: SSL_CERT_FILE replaces the container's own store")
	assert.NotContains(t, names, "registry.example.com", "a leaf certificate is not a trust anchor")
	assert.NotContains(t, names, "Expired Root")
}

func TestBuildPEMDeduplicatesAndIsOrderStable(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)

	first := testCert(t, "Root A", true, future)
	second := testCert(t, "Root B", true, future)

	// The same certificate reaches buildPEM from both the host store and the
	// endpoint probe on any correctly provisioned device.
	oneWay := buildPEM([][]byte{first, second}, [][]byte{first})
	otherWay := buildPEM([][]byte{second}, [][]byte{first, second})

	assert.Len(t, commonNames(t, oneWay), 2, "a certificate present in both inputs must appear once")
	assert.Equal(t, oneWay, otherWay, "byte-identical trust sets must produce byte-identical bundles")
}

func TestWriteBundleIsAtomicAndSkipsUnchangedContent(t *testing.T) {
	appsDir := t.TempDir()
	bundle := buildPEM([][]byte{testCert(t, "Root", true, time.Now().Add(time.Hour))})

	path, err := writeBundle(appsDir, bundle)
	require.NoError(t, err)
	assert.Equal(t, HostPath(appsDir), path)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, bundle, written)

	info, err := os.Stat(path)
	require.NoError(t, err)
	if os.Getenv("GOOS") != "windows" {
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(),
			"an app container runs as whatever UID its image declares and must be able to read this")
	}

	firstModTime := info.ModTime()
	time.Sleep(20 * time.Millisecond)

	_, err = writeBundle(appsDir, bundle)
	require.NoError(t, err)

	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, firstModTime, info.ModTime(), "an unchanged bundle must not be rewritten")

	entries, err := os.ReadDir(HostDir(appsDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file may be left behind")
}

func TestAvailableAndRemove(t *testing.T) {
	appsDir := t.TempDir()
	assert.False(t, Available(appsDir), "a device that never built a bundle must mount nothing")

	_, err := writeBundle(appsDir, buildPEM([][]byte{testCert(t, "Root", true, time.Now().Add(time.Hour))}))
	require.NoError(t, err)
	assert.True(t, Available(appsDir))

	require.NoError(t, Remove(appsDir))
	assert.False(t, Available(appsDir))
	assert.NoError(t, Remove(appsDir), "removing an absent bundle is not an error")
}

func TestAvailableRejectsAnEmptyBundle(t *testing.T) {
	appsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(HostDir(appsDir), 0o755))
	require.NoError(t, os.WriteFile(HostPath(appsDir), nil, 0o644))

	assert.False(t, Available(appsDir), "an empty file would silently strip a container's trust store")
}

func TestEnabledOnlyForApplianceDomainDevices(t *testing.T) {
	assert.True(t, Enabled("tls-sf015.corp.trumpf.com"))
	assert.False(t, Enabled(""), "the cloud fleet keeps its images' own trust stores")
	assert.False(t, Enabled("   "))
}

func TestTLSAddress(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		expected string
	}{
		{"wss without a port defaults to 443", "wss://ws.tls-sf015.corp.trumpf.com/ws", "ws.tls-sf015.corp.trumpf.com:443"},
		{"explicit port is kept", "wss://ws.example.com:8443/ws", "ws.example.com:8443"},
		{"https is probed too", "https://registry.example.com/v2/", "registry.example.com:443"},
		{"plain ws has no private CA to distribute", "ws://192.168.1.10:18080/ws", ""},
		{"empty endpoint", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, err := tlsAddress(test.in)
			require.NoError(t, err)
			assert.Equal(t, test.expected, address)
		})
	}
}

func TestDecodePEMCertificatesSkipsNonCertificateBlocks(t *testing.T) {
	der := testCert(t, "Root", true, time.Now().Add(time.Hour))
	bundle := buildPEM([][]byte{der})

	withNoise := append([]byte("-----BEGIN PRIVATE KEY-----\nZm9v\n-----END PRIVATE KEY-----\n"), bundle...)

	ders := decodePEMCertificates(withNoise)
	require.Len(t, ders, 1)
	assert.Equal(t, der, ders[0])
}

func TestHostRootDERsReadsTheRunnersOwnStore(t *testing.T) {
	ders, err := hostRootDERs()
	if err != nil {
		t.Skipf("no host CA store on this machine: %s", err)
	}

	require.NotEmpty(t, ders)
	assert.NotEmpty(t, buildPEM(ders), "the host store must yield at least one usable CA")
}

func TestContainerPathsAreUnderTheMountedDirectory(t *testing.T) {
	assert.Equal(t, ContainerDir, filepath.ToSlash(filepath.Dir(ContainerPath)),
		"the bundle must live inside the directory that gets bind-mounted")
}
