package trust

import (
	_ "embed"
	"fmt"
	"path/filepath"
)

// The tunnel CA bundle is what frpc verifies the frps certificate against
// (transport.tls.trustedCaFile). Without one frpc sets InsecureSkipVerify (frp
// v0.71.0 pkg/transport/tls.go NewClientTLSConfig), so anyone on the path
// could pose as the tunnel server and collect the device's tunnel_proof.
//
// frpc builds its pool from this file ALONE, not on top of the system roots,
// so the bundle has to trust at least what the agent's WAMP connection trusts.
// It carries:
//   - the whole host store (which holds the operator's corporate root on a
//     domain-mode appliance device);
//   - the CA chain the device endpoint presents, verified by the host first,
//     as Build folds it into the app bundle. The appliance's ingress serves
//     the same chain on ws.<domain> and on the apex frpc dials. This is also
//     what covers Windows, where an exported store misses what the platform
//     verifier fetches on demand: intermediates via AIA and roots Windows
//     installs only when first needed (automatic root update);
//   - the Let's Encrypt roots, so the cloud frps still verifies on a minimal
//     system whose store is missing or stale.

// tunnelCAFileName sits in the agent directory next to frpc.yaml. Unlike the
// app bundle it is read only by frpc, which the agent spawns itself.
const tunnelCAFileName = "tunnel-ca.pem"

//go:embed letsencrypt-roots.pem
var letsEncryptRootsPEM []byte

// TunnelCAPath returns where the tunnel CA bundle lives for agentDir.
func TunnelCAPath(agentDir string) string {
	return filepath.Join(agentDir, tunnelCAFileName)
}

// HostRoots returns the CA certificates of the host trust store, as DER. The
// error is non-nil when no store could be read; the tunnel bundle is then
// built from the embedded roots alone.
func HostRoots() ([][]byte, error) {
	return hostRootDERs()
}

// EndpointCAs returns the CA certificates of the chain the device endpoint
// presents, as verified by the host, or nothing for a non-TLS endpoint. See
// endpointCADERs.
func EndpointCAs(endpointURL string) ([][]byte, error) {
	return endpointCADERs(endpointURL)
}

// WriteTunnelCA writes the tunnel CA bundle for agentDir from hostRoots and
// endpointCAs (see HostRoots and EndpointCAs) plus the embedded Let's Encrypt
// roots and returns its path. The file is rewritten only when its content
// changes.
func WriteTunnelCA(agentDir string, hostRoots, endpointCAs [][]byte) (string, error) {
	bundle := buildPEM(hostRoots, endpointCAs, decodePEMCertificates(letsEncryptRootsPEM))
	if len(bundle) == 0 {
		return "", fmt.Errorf("no usable CA certificates for the tunnel bundle")
	}
	return writeFileAtomic(agentDir, tunnelCAFileName, bundle)
}
