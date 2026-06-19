package keyvault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const keysBucket = "keyvault.keys"

// JSONWebKey replica el subconjunto de JWK que devuelve Key Vault para una
// key (solo lo necesario para que az/Terraform vean una respuesta con
// forma correcta — no es material criptográfico real, ver newFakeKeyMaterial).
type JSONWebKey struct {
	Kid    string   `json:"kid"`
	Kty    string   `json:"kty"`
	KeyOps []string `json:"key_ops,omitempty"`
	N      string   `json:"n,omitempty"`
	E      string   `json:"e,omitempty"`
}

// KeyAttributes replica "attributes" en la respuesta real de Key Vault.
type KeyAttributes struct {
	Enabled bool  `json:"enabled"`
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Key replica la forma estándar del data plane de Key Vault para una key
// (GET/PUT https://{vault}.vault.azure.net/keys/{name}).
type Key struct {
	Key        JSONWebKey    `json:"key"`
	Attributes KeyAttributes `json:"attributes"`
}

type keyRequest struct {
	Kty     string   `json:"kty"`
	KeySize int      `json:"key_size,omitempty"`
	KeyOps  []string `json:"key_ops,omitempty"`
}

func keyKey(vault, name string) string {
	return vault + "/" + name
}

func keyID(r *http.Request, vault, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.vault/keys/%s", scheme, r.Host, vault, name)
}

// newFakeKeyMaterial genera bytes aleatorios codificados en base64url para
// rellenar los campos "n"/"e" de un JWK RSA simulado. El emulador no
// implementa operaciones criptográficas reales (sign/encrypt/wrapKey),
// solo el almacenamiento y la forma de la respuesta — suficiente para que
// az/Terraform creen y lean keys end to end.
func newFakeKeyMaterial() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("keyvault: error generando material de key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newKeyVersion() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("keyvault: error generando version: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// handleKeys despacha las rutas bajo /{vault}.vault/keys[/{name}].
func (s *Service) handleKeys(w http.ResponseWriter, r *http.Request, vault, name string) {
	if name == "" {
		if r.Method != http.MethodGet {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"a nivel de colección solo se soporta GET (list keys)")
			return
		}
		s.listKeys(w, r, vault)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.createKey(w, r, vault, name)
	case http.MethodGet:
		s.getKey(w, r, vault, name)
	case http.MethodDelete:
		s.deleteKey(w, r, vault, name)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para una key")
	}
}

func (s *Service) createKey(w http.ResponseWriter, r *http.Request, vault, name string) {
	var req keyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Kty) == "" {
		req.Kty = "RSA"
	}

	version, err := newKeyVersion()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	n, err := newFakeKeyMaterial()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	now := time.Now().UTC().Unix()
	key := secretLikeKeyAttributes(now)
	rec := Key{
		Key: JSONWebKey{
			Kid:    keyID(r, vault, name) + "/" + version,
			Kty:    req.Kty,
			KeyOps: req.KeyOps,
			N:      n,
			E:      "AQAB",
		},
		Attributes: key,
	}

	if err := s.db.Put(keysBucket, keyKey(vault, name), rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, rec)
}

func secretLikeKeyAttributes(now int64) KeyAttributes {
	return KeyAttributes{Enabled: true, Created: now, Updated: now}
}

func (s *Service) getKey(w http.ResponseWriter, r *http.Request, vault, name string) {
	var rec Key
	found, err := s.db.Get(keysBucket, keyKey(vault, name), &rec)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "KeyNotFound",
			fmt.Sprintf("la key '%s' no existe en el vault '%s'", name, vault))
		return
	}
	server.WriteJSON(w, http.StatusOK, rec)
}

func (s *Service) listKeys(w http.ResponseWriter, r *http.Request, vault string) {
	keys := make([]Key, 0)
	err := s.db.List(keysBucket, vault+"/", func(key string, raw []byte) error {
		var rec Key
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		keys = append(keys, rec)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": keys})
}

func (s *Service) deleteKey(w http.ResponseWriter, r *http.Request, vault, name string) {
	key := keyKey(vault, name)
	found, err := s.db.Get(keysBucket, key, &Key{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "KeyNotFound",
			fmt.Sprintf("la key '%s' no existe en el vault '%s'", name, vault))
		return
	}
	if err := s.db.Delete(keysBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
