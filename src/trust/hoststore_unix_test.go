//go:build !windows && !darwin

package trust

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SSL_CERT_FILE and SSL_CERT_DIR feed Go's pool, and with it the WAMP
// connection, so they must feed the tunnel bundle too. Added to the standard
// locations rather than replacing them: never trust less than Go does.
func TestHostRootDERsAddsSSLCertFileAndDir(t *testing.T) {
	future := time.Now().Add(365 * 24 * time.Hour)
	fixtures := t.TempDir()

	certFile := filepath.Join(fixtures, "extra-bundle.pem")
	require.NoError(t, os.WriteFile(certFile, buildPEM([][]byte{testCert(t, "SSL_CERT_FILE Root", true, future)}), 0o644))
	certDir := filepath.Join(fixtures, "extra-dir")
	require.NoError(t, os.MkdirAll(certDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(certDir, "root.pem"), buildPEM([][]byte{testCert(t, "SSL_CERT_DIR Root", true, future)}), 0o644))

	t.Setenv("SSL_CERT_FILE", certFile)
	t.Setenv("SSL_CERT_DIR", filepath.Join(fixtures, "missing")+string(os.PathListSeparator)+certDir)

	ders, err := hostRootDERs()
	require.NoError(t, err)
	names := commonNames(t, buildPEM(ders))
	assert.Contains(t, names, "SSL_CERT_FILE Root")
	assert.Contains(t, names, "SSL_CERT_DIR Root")
}
