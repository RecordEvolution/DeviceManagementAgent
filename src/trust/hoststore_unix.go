//go:build !windows && !darwin

package trust

import (
	"os"
	"path/filepath"
)

// bundleCandidates are the usual single-file CA bundles, in the order the
// major distributions install them. FlockOS and Debian devices land on the
// first entry; the rest cover RPM, SUSE and Alpine hosts.
var bundleCandidates = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/ca-bundle.pem",
	"/etc/pki/tls/cacert.pem",
	"/etc/ssl/cert.pem",
}

// dirCandidates are hashed-certificate directories, the fallback for hosts
// that ship no concatenated bundle at all.
var dirCandidates = []string{
	"/etc/ssl/certs",
	"/etc/pki/tls/certs",
}

func hostRootDERs() ([][]byte, error) {
	for _, path := range bundleCandidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if ders := decodePEMCertificates(data); len(ders) > 0 {
			return ders, nil
		}
	}

	for _, dir := range dirCandidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		var ders [][]byte
		for _, entry := range entries {
			switch filepath.Ext(entry.Name()) {
			case ".crt", ".pem", ".cer":
			default:
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			ders = append(ders, decodePEMCertificates(data)...)
		}
		if len(ders) > 0 {
			return ders, nil
		}
	}

	return nil, errNoHostStore
}
