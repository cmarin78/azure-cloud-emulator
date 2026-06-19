package keyvault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const secretsBucket = "keyvault.secrets"

// SecretAttributes replica "attributes" en la respuesta real de Key Vault
// (enabled/created/updated). No se modela expiración/activación: el
// emulador no necesita esa fidelidad para los smoke tests de az/Terraform.
type SecretAttributes struct {
	Enabled bool  `json:"enabled"`
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Secret replica la forma estándar del data plane de Key Vault para un
// secret (GET/PUT https://{vault}.vault.azure.net/secrets/{name}).
type Secret struct {
	ID         string           `json:"id"`
	Name       string           `json:"name,omitempty"`
	Value      string           `json:"value"`
	Attributes SecretAttributes `json:"attributes"`
}

type secretRequest struct {
	Value string `json:"value"`
}

func secretKey(vault, name string) string {
	return vault + "/" + name
}

func secretID(r *http.Request, vault, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.vault/secrets/%s", scheme, r.Host, vault, name)
}

// handleSecrets despacha las rutas bajo /{vault}.vault/secrets[/{name}].
func (s *Service) handleSecrets(w http.ResponseWriter, r *http.Request, vault, name string) {
	if name == "" {
		if r.Method != http.MethodGet {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"a nivel de colección solo se soporta GET (list secrets)")
			return
		}
		s.listSecrets(w, r, vault)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.setSecret(w, r, vault, name)
	case http.MethodGet:
		s.getSecret(w, r, vault, name)
	case http.MethodDelete:
		s.deleteSecret(w, r, vault, name)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para un secret")
	}
}

func (s *Service) setSecret(w http.ResponseWriter, r *http.Request, vault, name string) {
	var req secretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	now := time.Now().UTC().Unix()
	key := secretKey(vault, name)
	var existing Secret
	found, err := s.db.Get(secretsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	created := now
	if found {
		created = existing.Attributes.Created
	}

	secret := Secret{
		ID:    secretID(r, vault, name),
		Name:  name,
		Value: req.Value,
		Attributes: SecretAttributes{
			Enabled: true,
			Created: created,
			Updated: now,
		},
	}
	if err := s.db.Put(secretsBucket, key, secret); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, secret)
}

func (s *Service) getSecret(w http.ResponseWriter, r *http.Request, vault, name string) {
	var secret Secret
	found, err := s.db.Get(secretsBucket, secretKey(vault, name), &secret)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "SecretNotFound",
			fmt.Sprintf("el secret '%s' no existe en el vault '%s'", name, vault))
		return
	}
	server.WriteJSON(w, http.StatusOK, secret)
}

func (s *Service) listSecrets(w http.ResponseWriter, r *http.Request, vault string) {
	secrets := make([]Secret, 0)
	err := s.db.List(secretsBucket, vault+"/", func(key string, raw []byte) error {
		var secret Secret
		if err := json.Unmarshal(raw, &secret); err != nil {
			return err
		}
		// Igual que la API real: el listado no incluye el "value" del
		// secret, solo su id/atributos.
		secret.Value = ""
		secrets = append(secrets, secret)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": secrets})
}

func (s *Service) deleteSecret(w http.ResponseWriter, r *http.Request, vault, name string) {
	key := secretKey(vault, name)
	found, err := s.db.Get(secretsBucket, key, &Secret{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "SecretNotFound",
			fmt.Sprintf("el secret '%s' no existe en el vault '%s'", name, vault))
		return
	}
	if err := s.db.Delete(secretsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
