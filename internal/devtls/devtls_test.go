package devtls

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSelfSigned(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// 1. Generation
	if err := EnsureSelfSigned(certPath, keyPath); err != nil {
		t.Fatalf("EnsureSelfSigned failed: %v", err)
	}

	certData, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read cert file: %v", err)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read key file: %v", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("expected CERTIFICATE PEM block, got %v", block)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse x509 certificate: %v", err)
	}

	if cert.Subject.CommonName != "azure-emulator (dev, self-signed)" {
		t.Errorf("unexpected CommonName: %q", cert.Subject.CommonName)
	}
	if !cert.IsCA {
		t.Errorf("expected IsCA=true")
	}

	// Verify DNSNames & IPs
	hasLocalhost := false
	for _, name := range cert.DNSNames {
		if name == "localhost" {
			hasLocalhost = true
			break
		}
	}
	if !hasLocalhost {
		t.Errorf("expected DNSNames to contain 'localhost', got %v", cert.DNSNames)
	}

	hasIPv4 := false
	hasIPv6 := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasIPv4 = true
		}
		if ip.Equal(net.ParseIP("::1")) {
			hasIPv6 = true
		}
	}
	if !hasIPv4 || !hasIPv6 {
		t.Errorf("expected 127.0.0.1 and ::1 in IPAddresses, got %v", cert.IPAddresses)
	}

	// 2. Idempotence: calling again when files exist should return nil and not re-generate
	if err := EnsureSelfSigned(certPath, keyPath); err != nil {
		t.Fatalf("idempotent EnsureSelfSigned failed: %v", err)
	}
	certDataAfter, _ := os.ReadFile(certPath)
	keyDataAfter, _ := os.ReadFile(keyPath)
	if string(certData) != string(certDataAfter) || string(keyData) != string(keyDataAfter) {
		t.Errorf("expected identical cert/key data on idempotent call")
	}
}
