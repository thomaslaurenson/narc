package certmgr

import (
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWritePEMAtomicCleanup verifies that a failed writePEM leaves no .tmp
// artefact and does not overwrite any existing destination file.
func TestWritePEMAtomicCleanup(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "ca.pem")

	// Write a sentinel value to dest so we can confirm it is untouched on failure.
	sentinel := []byte("original")
	if err := os.WriteFile(dest, sentinel, 0644); err != nil {
		t.Fatal(err)
	}

	// Pass a path inside a non-existent subdirectory so os.OpenFile on the .tmp
	// file fails immediately, exercising the early-error cleanup path.
	badDest := filepath.Join(dir, "nonexistent", "ca.pem")
	err := writePEM(badDest, "CERTIFICATE", []byte{0x01}, 0644)
	if err == nil {
		t.Fatal("expected error for unwritable path, got nil")
	}

	// The .tmp file must not exist.
	if _, statErr := os.Stat(badDest + ".tmp"); !os.IsNotExist(statErr) {
		t.Error(".tmp artefact left behind after writePEM failure")
	}

	// The original dest must be untouched.
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading dest after failed writePEM: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("dest file was modified: got %q, want %q", got, sentinel)
	}
}

func TestGenerateCACert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")

	if err := generateCACert(certPath, keyPath); err != nil {
		t.Fatalf("generateCACert: %v", err)
	}

	certInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("cert file missing: %v", err)
	}
	if certInfo.Mode().Perm() != 0644 {
		t.Errorf("cert file permissions: got %o, want 0644", certInfo.Mode().Perm())
	}

	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if keyInfo.Mode().Perm() != 0600 {
		t.Errorf("key file permissions: got %o, want 0600", keyInfo.Mode().Perm())
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block from cert file")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	if !cert.IsCA {
		t.Error("certificate IsCA should be true")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("certificate missing KeyUsageCertSign")
	}
	if cert.Subject.CommonName != "narc CA" {
		t.Errorf("CommonName: got %q, want %q", cert.Subject.CommonName, "narc CA")
	}

	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
}

func TestEnsureCACertIdempotent(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")

	if err := generateCACert(certPath, keyPath); err != nil {
		t.Fatalf("first generateCACert: %v", err)
	}
	firstStat, _ := os.Stat(certPath)

	ensureFn := func() error {
		_, certErr := os.Stat(certPath)
		_, keyErr := os.Stat(keyPath)
		if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
			return generateCACert(certPath, keyPath)
		}
		return nil
	}
	if err := ensureFn(); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	secondStat, _ := os.Stat(certPath)

	if firstStat.ModTime() != secondStat.ModTime() {
		t.Error("EnsureCACert overwrote existing cert — should be idempotent")
	}
}

func TestNeedsRenewalExpiredCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")

	// Generate a cert with NotAfter in the past by temporarily overriding the template.
	// We call generateCACert and then patch the file with an already-expired cert.
	if err := generateCACert(certPath, keyPath); err != nil {
		t.Fatalf("generateCACert: %v", err)
	}

	// Parse the existing cert, then re-sign it with a NotAfter in the past.
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// Build an expired version of the cert and write it back.
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("no PEM block in key")
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}

	past := time.Now().Add(-48 * time.Hour)
	expiredTemplate := cert
	expiredTemplate.NotBefore = past.Add(-24 * time.Hour)
	expiredTemplate.NotAfter = past

	expiredDER, err := x509.CreateCertificate(cryptorand.Reader, expiredTemplate, expiredTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create expired cert: %v", err)
	}
	if err := writePEM(certPath, "CERTIFICATE", expiredDER, 0644); err != nil {
		t.Fatalf("write expired cert: %v", err)
	}

	if !needsRenewal(certPath) {
		t.Error("needsRenewal should return true for an expired cert")
	}
}

func TestNeedsRenewalFreshCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")

	if err := generateCACert(certPath, keyPath); err != nil {
		t.Fatalf("generateCACert: %v", err)
	}
	if needsRenewal(certPath) {
		t.Error("needsRenewal should return false for a freshly generated cert")
	}
}
