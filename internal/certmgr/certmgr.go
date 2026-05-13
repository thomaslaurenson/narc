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
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/thomaslaurenson/narc/internal/config"
)

const (
	caCertFilename  = "ca.pem"
	caKeyFilename   = "ca.key"
	certValidYears  = 2
	certRenewBefore = 30 * 24 * time.Hour
)

// CACertPath returns the path to the narc CA certificate file.
func CACertPath() (string, error) {
	dir, err := config.NarcDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, caCertFilename), nil
}

func caKeyPath() (string, error) {
	dir, err := config.NarcDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, caKeyFilename), nil
}

// EnsureCACert creates or rotates the CA certificate and key if they are
// absent or nearing expiry.
func EnsureCACert() error {
	// Ensure the directory exists before we attempt any writes.
	if _, err := config.NarcDir(); err != nil {
		return err
	}

	certPath, err := CACertPath()
	if err != nil {
		return err
	}
	keyPath, err := caKeyPath()
	if err != nil {
		return err
	}

	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)

	if errors.Is(certErr, os.ErrNotExist) || errors.Is(keyErr, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "[narc] Generating CA certificate...\n")
		return generateCACert(certPath, keyPath)
	}

	// Rotate the cert if it expires soon.
	if needsRenewal(certPath) {
		fmt.Fprintf(os.Stderr, "[narc] CA certificate expires soon - regenerating...\n")
		return generateCACert(certPath, keyPath)
	}
	return nil
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

// LoadTLSCert loads the CA certificate and key pair from disk and returns it
// as a tls.Certificate ready for use with goproxy.
func LoadTLSCert() (tls.Certificate, error) {
	certPath, err := CACertPath()
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPath, err := caKeyPath()
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
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
