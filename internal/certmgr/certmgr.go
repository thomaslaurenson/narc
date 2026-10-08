// Package certmgr manages the narc CA certificate and key used to intercept
// TLS traffic. It generates, stores, and rotates the CA as needed.
package certmgr

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFilename  = "ca.pem"
	caKeyFilename   = "ca.key"
	certValidYears  = 2
	certRenewBefore = 30 * 24 * time.Hour
)

// Status reports what EnsureCACert did to the CA.
type Status int

const (
	// StatusCurrent means the existing CA was valid and was kept.
	StatusCurrent Status = iota
	// StatusCreated means no CA existed, so one was generated.
	StatusCreated
	// StatusRenewed means the CA was near expiry, so it was regenerated.
	StatusRenewed
)

// CACertPath returns the path of the CA certificate in dir.
func CACertPath(dir string) string {
	return filepath.Join(dir, caCertFilename)
}

func caKeyPath(dir string) string {
	return filepath.Join(dir, caKeyFilename)
}

// EnsureCACert creates the CA certificate and key in dir when either is absent,
// regenerates them when the certificate is near expiry, and reports which it
// did.
func EnsureCACert(dir string) (Status, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return StatusCurrent, err
	}

	certPath := CACertPath(dir)
	keyPath := caKeyPath(dir)

	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)

	if errors.Is(certErr, fs.ErrNotExist) || errors.Is(keyErr, fs.ErrNotExist) {
		return StatusCreated, generateCACert(certPath, keyPath)
	}
	if needsRenewal(certPath) {
		return StatusRenewed, generateCACert(certPath, keyPath)
	}
	return StatusCurrent, nil
}

// needsRenewal reports whether the PEM cert at path is missing, unparseable,
// or expires within certRenewBefore.
func needsRenewal(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	return cert.NotAfter.Before(time.Now().Add(certRenewBefore))
}

// LoadTLSCert loads the CA certificate and key pair from dir, with Leaf
// populated, since goproxy signs each per-host certificate with the Leaf.
func LoadTLSCert(dir string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(CACertPath(dir), caKeyPath(dir))
	if err != nil {
		return tls.Certificate{}, err
	}
	if cert.Leaf == nil {
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("parse CA certificate %q: %w", CACertPath(dir), err)
		}
	}
	return cert, nil
}

func generateCACert(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate EC key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "narc CA",
			Organization: []string{"narc"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(certValidYears, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", certDER, 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal EC key: %w", err)
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	return nil
}

func writePEM(path, pemType string, der []byte, mode os.FileMode) (err error) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if err = pem.Encode(f, &pem.Block{Type: pemType, Bytes: der}); err != nil {
		return err
	}
	// Close explicitly before Rename so the file descriptor is flushed on all platforms.
	closed = true
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
