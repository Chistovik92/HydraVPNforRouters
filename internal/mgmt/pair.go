package mgmt

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// CertFingerprint returns the SHA-256 of the first certificate in a PEM
// file (hex). Clients pin it instead of trusting a self-signed certificate.
func CertFingerprint(pemFile string) (string, error) {
	data, err := os.ReadFile(pemFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("%s: no PEM certificate", pemFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// PairURI builds the link that a client scans or opens to add this router:
// hydravpn-router://host:port?token=...&tls=1&fp=<sha256>.
func PairURI(s config.Settings, host, token string) (string, error) {
	_, port, err := net.SplitHostPort(s.APIListen)
	if err != nil {
		return "", fmt.Errorf("api_listen is not set or invalid: %w", err)
	}
	q := url.Values{"token": {token}}
	if s.APITLSCert != "" && s.APITLSKey != "" {
		q.Set("tls", "1")
		fp, err := CertFingerprint(s.APITLSCert)
		if err != nil {
			return "", err
		}
		q.Set("fp", fp)
	} else {
		q.Set("tls", "0")
	}
	u := url.URL{Scheme: "hydravpn-router", Host: net.JoinHostPort(host, port), RawQuery: q.Encode()}
	return u.String(), nil
}
