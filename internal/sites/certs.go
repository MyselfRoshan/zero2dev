package sites

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"time"

	"zero2dev/internal/fsutil"
)

// NginxCAPath is the root certificate a user can trust so that HTTPS sites
// served by nginx open without a browser warning. Nothing is installed into
// the OS trust store by Zero2Dev.
func NginxCAPath(home string) string { return filepath.Join(home, "nginx", "certs", "ca.crt") }

const (
	caLifetime   = 10 * 365 * 24 * time.Hour
	leafLifetime = 397 * 24 * time.Hour // within the browser limit for server certificates
	renewBefore  = 30 * 24 * time.Hour
)

var now = time.Now // replaced in tests

// ensureNginxCerts makes sure <home>/nginx/certs holds a local CA and a
// server certificate (signed by it) covering exactly domains. The CA is
// created once and kept, so trusting it stays valid across changes. The
// server certificate is reissued when the names change, it expires soon, or
// it was not signed by the current CA. With no domains nothing is done.
func ensureNginxCerts(home string, domains []string) error {
	if len(domains) == 0 {
		return nil
	}
	dir := filepath.Join(home, "nginx", "certs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ca, caKey, err := loadOrCreateCA(dir)
	if err != nil {
		return fmt.Errorf("local CA: %w", err)
	}
	if leafOK(filepath.Join(dir, "sites.crt"), filepath.Join(dir, "sites.key"), ca, domains) {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: domains[0], Organization: []string{"Zero2Dev local sites"}},
		DNSNames:     domains,
		NotBefore:    now().Add(-time.Hour),
		NotAfter:     now().Add(leafLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writeKey(filepath.Join(dir, "sites.key"), key); err != nil {
		return err
	}
	return writeCert(filepath.Join(dir, "sites.crt"), der)
}

func loadOrCreateCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath, keyPath := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	if c, k, err := loadPair(certPath, keyPath); err == nil && c.IsCA && now().Add(renewBefore).Before(c.NotAfter) {
		return c, k, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Zero2Dev Local CA", Organization: []string{"Zero2Dev"}},
		NotBefore:             now().Add(-time.Hour),
		NotAfter:              now().Add(caLifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	if err := writeKey(keyPath, key); err != nil {
		return nil, nil, err
	}
	if err := writeCert(certPath, der); err != nil {
		return nil, nil, err
	}
	c, err := x509.ParseCertificate(der)
	return c, key, err
}

func leafOK(certPath, keyPath string, ca *x509.Certificate, domains []string) bool {
	c, _, err := loadPair(certPath, keyPath)
	if err != nil || c.CheckSignatureFrom(ca) != nil || !now().Add(renewBefore).Before(c.NotAfter) {
		return false
	}
	names := slices.Clone(c.DNSNames)
	slices.Sort(names)
	return slices.Equal(names, domains)
}

func loadPair(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	kb, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	cp, _ := pem.Decode(cb)
	kp, _ := pem.Decode(kb)
	if cp == nil || kp == nil {
		return nil, nil, errors.New("not PEM")
	}
	c, err := x509.ParseCertificate(cp.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParseECPrivateKey(kp.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if !k.PublicKey.Equal(c.PublicKey) {
		return nil, nil, errors.New("key does not match certificate")
	}
	return c, k, nil
}

func writeKey(path string, k *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}

func writeCert(path string, der []byte) error {
	return fsutil.WriteFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err) // the OS RNG failing is unrecoverable
	}
	return n
}
