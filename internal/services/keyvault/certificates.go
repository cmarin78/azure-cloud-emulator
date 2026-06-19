package keyvault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const certificatesBucket = "keyvault.certificates"

// CertificateAttributes replica "attributes" en la respuesta real de Key
// Vault para un certificate.
type CertificateAttributes struct {
	Enabled bool  `json:"enabled"`
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Certificate replica la forma estándar del data plane de Key Vault para
// un certificate (GET/PUT https://{vault}.vault.azure.net/certificates/{name}).
// "Cer" lleva el material "PEM-like" simulado en base64, igual de no-real
// que el JWK de keys.go: suficiente para que az/Terraform vean una
// respuesta con la forma correcta, sin implementar X.509 real.
type Certificate struct {
	ID         string                `json:"id"`
	Cer        string                `json:"cer"`
	Thumbprint string                `json:"x5t"`
	Attributes CertificateAttributes `json:"attributes"`
	Policy     map[string]any        `json:"policy,omitempty"`
}

type certificateRequest struct {
	Policy map[string]any `json:"policy,omitempty"`
}

func certificateKey(vault, name string) string {
	return vault + "/" + name
}

func certificateID(r *http.Request, vault, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.vault/certificates/%s", scheme, r.Host, vault, name)
}

func newFakeCertMaterial() (string, string, error) {
	cer := make([]byte, 64)
	if _, err := rand.Read(cer); err != nil {
		return "", "", fmt.Errorf("keyvault: error generando material de certificado: %w", err)
	}
	thumb := make([]byte, 20) // mismo tamaño que un thumbprint SHA-1 real
	if _, err := rand.Read(thumb); err != nil {
		return "", "", fmt.Errorf("keyvault: error generando thumbprint: %w", err)
	}
	return base64.StdEncoding.EncodeToString(cer), hex.EncodeToString(thumb), nil
}

// handleCertificates despacha las rutas bajo /{vault}.vault/certificates[/{name}].
func (s *Service) handleCertificates(w http.ResponseWriter, r *http.Request, vault, name string) {
	if name == "" {
		if r.Method != http.MethodGet {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"a nivel de colección solo se soporta GET (list certificates)")
			return
		}
		s.listCertificates(w, r, vault)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.createCertificate(w, r, vault, name)
	case http.MethodGet:
		s.getCertificate(w, r, vault, name)
	case http.MethodDelete:
		s.deleteCertificate(w, r, vault, name)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para un certificate")
	}
}

func (s *Service) createCertificate(w http.ResponseWriter, r *http.Request, vault, name string) {
	var req certificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	cer, thumb, err := newFakeCertMaterial()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	now := time.Now().UTC().Unix()
	cert := Certificate{
		ID:         certificateID(r, vault, name),
		Cer:        cer,
		Thumbprint: thumb,
		Attributes: CertificateAttributes{Enabled: true, Created: now, Updated: now},
		Policy:     req.Policy,
	}
	if err := s.db.Put(certificatesBucket, certificateKey(vault, name), cert); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, cert)
}

func (s *Service) getCertificate(w http.ResponseWriter, r *http.Request, vault, name string) {
	var cert Certificate
	found, err := s.db.Get(certificatesBucket, certificateKey(vault, name), &cert)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "CertificateNotFound",
			fmt.Sprintf("el certificate '%s' no existe en el vault '%s'", name, vault))
		return
	}
	server.WriteJSON(w, http.StatusOK, cert)
}

func (s *Service) listCertificates(w http.ResponseWriter, r *http.Request, vault string) {
	certs := make([]Certificate, 0)
	err := s.db.List(certificatesBucket, vault+"/", func(key string, raw []byte) error {
		var cert Certificate
		if err := json.Unmarshal(raw, &cert); err != nil {
			return err
		}
		certs = append(certs, cert)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": certs})
}

func (s *Service) deleteCertificate(w http.ResponseWriter, r *http.Request, vault, name string) {
	key := certificateKey(vault, name)
	found, err := s.db.Get(certificatesBucket, key, &Certificate{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "CertificateNotFound",
			fmt.Sprintf("el certificate '%s' no existe en el vault '%s'", name, vault))
		return
	}
	if err := s.db.Delete(certificatesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
