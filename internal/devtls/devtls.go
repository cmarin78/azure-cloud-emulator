// Package devtls genera (y reutiliza) un certificado TLS autofirmado para
// que el emulador pueda servir HTTPS localmente. Esto existe únicamente
// porque tanto az CLI (vía MSAL, que valida que cualquier "authority" de
// OAuth2 use https) como el provider de Terraform `azurerm` (que antepone
// "https://" a `metadata_host` sin posibilidad de configurarlo) rechazan
// de forma irrecuperable un cloud personalizado servido en HTTP plano —
// confirmado empíricamente: `az login --service-principal` falla con
// "should consist of an https url..." desde msal/authority.py, y
// `terraform plan` con azurerm falla con "http: server gave HTTP response
// to HTTPS client". No hay flag de ninguno de los dos clientes para
// evitarlo; servir TLS es la única forma de que el flujo completo
// (descubrimiento de metadata + login + llamadas ARM) funcione de punta a
// punta contra este emulador.
//
// El certificado es autofirmado y NO se valida automáticamente por los
// clientes: el usuario debe confiar en él explícitamente (ver
// "Habilitar HTTPS" en README.md). Esto es intencional — el emulador
// nunca debe parecer una CA de confianza por defecto.
package devtls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureSelfSigned garantiza que certPath/keyPath existan: si ya existen,
// los reutiliza tal cual (para que el usuario no tenga que volver a
// confiar en un certificado nuevo cada vez que reinicia el emulador). Si
// no existen, genera un par RSA-2048 autofirmado válido por 10 años para
// "localhost", "127.0.0.1" y "::1", y lo escribe en disco en formato PEM.
func EnsureSelfSigned(certPath, keyPath string) error {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if certErr == nil && keyErr == nil {
		return nil
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("devtls: no se pudo generar la llave RSA: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("devtls: no se pudo generar el número de serie del certificado: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "azure-emulator (dev, self-signed)", Organization: []string{"azure-emulator"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("devtls: no se pudo crear el certificado: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return fmt.Errorf("devtls: no se pudo crear el directorio para el certificado: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return fmt.Errorf("devtls: no se pudo crear el directorio para la llave: %w", err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("devtls: no se pudo crear %s: %w", certPath, err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("devtls: no se pudo escribir %s: %w", certPath, err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("devtls: no se pudo crear %s: %w", keyPath, err)
	}
	defer keyOut.Close()
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("devtls: no se pudo escribir %s: %w", keyPath, err)
	}

	return nil
}
