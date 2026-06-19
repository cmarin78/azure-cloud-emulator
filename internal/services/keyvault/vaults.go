package keyvault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const vaultsBucket = "keyvault.vaults"

// Sku replica "properties.sku" de un vault (p. ej. {"family": "A", "name": "standard"}).
type Sku struct {
	Family string `json:"family"`
	Name   string `json:"name"`
}

// AccessPolicyEntry replica un elemento de "properties.accessPolicies[]".
// El emulador no aplica ningún control de acceso real; se persiste y se
// devuelve tal cual para que az/Terraform no se quejen de un campo
// faltante, igual que con otros sub-objetos no enforced en este proyecto.
type AccessPolicyEntry struct {
	TenantID    string         `json:"tenantId"`
	ObjectID    string         `json:"objectId"`
	Permissions map[string]any `json:"permissions"`
}

// Vault replica la forma estándar de ARM para Microsoft.KeyVault/vaults.
type Vault struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties VaultProperties   `json:"properties"`
}

type VaultProperties struct {
	ProvisioningState    string              `json:"provisioningState"`
	Sku                  Sku                 `json:"sku"`
	TenantID             string              `json:"tenantId"`
	AccessPolicies       []AccessPolicyEntry `json:"accessPolicies"`
	VaultURI             string              `json:"vaultUri"`
	EnabledForDeployment bool                `json:"enabledForDeployment"`
}

type vaultRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		Sku                  Sku                 `json:"sku"`
		TenantID             string              `json:"tenantId"`
		AccessPolicies       []AccessPolicyEntry `json:"accessPolicies,omitempty"`
		EnabledForDeployment bool                `json:"enabledForDeployment,omitempty"`
	} `json:"properties"`
}

func vaultKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func vaultID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.KeyVault/vaults/%s", subID, rg, name)
}

// vaultURI apunta de vuelta al propio emulador, mismo truco "path-style"
// que storageaccounts.endpoints(): no hay un host real por vault, así que
// el data plane se direcciona con el nombre del vault como primer
// segmento del path (http://{emulador}/{vault}.vault/...).
func vaultURI(r *http.Request, vaultName string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.vault/", scheme, r.Host, vaultName)
}

func (s *Service) putVault(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")

	var req vaultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.Sku.Name) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.sku.name' es obligatorio (p. ej. 'standard')")
		return
	}
	if strings.TrimSpace(req.Properties.TenantID) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.tenantId' es obligatorio")
		return
	}

	key := vaultKey(subID, rg, name)
	_, found, err := s.getVault(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	accessPolicies := req.Properties.AccessPolicies
	if accessPolicies == nil {
		accessPolicies = make([]AccessPolicyEntry, 0)
	}

	vault := Vault{
		ID:       vaultID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.KeyVault/vaults",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: VaultProperties{
			ProvisioningState:    "Succeeded",
			Sku:                  req.Properties.Sku,
			TenantID:             req.Properties.TenantID,
			AccessPolicies:       accessPolicies,
			VaultURI:             vaultURI(r, name),
			EnabledForDeployment: req.Properties.EnabledForDeployment,
		},
	}

	if err := s.db.Put(vaultsBucket, key, vault); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, vault)
}

func (s *Service) getVault(subID, rg, name string) (Vault, bool, error) {
	var vault Vault
	found, err := s.db.Get(vaultsBucket, vaultKey(subID, rg, name), &vault)
	return vault, found, err
}

func (s *Service) getVaultHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")

	vault, found, err := s.getVault(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el vault '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, vault)
}

func (s *Service) listVaults(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	vaults := make([]Vault, 0)
	err := s.db.List(vaultsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var vault Vault
		if err := json.Unmarshal(raw, &vault); err != nil {
			return err
		}
		vaults = append(vaults, vault)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": vaults})
}

// deleteVault es idempotente (204 si no existe) y síncrono, igual que
// deleteVirtualNetwork: no valida ni cascada-borra los secrets/keys/
// certificates del data plane (misma simplificación que storage accounts
// no cascadea sus contenedores de blobs).
func (s *Service) deleteVault(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")
	key := vaultKey(subID, rg, name)

	found, err := s.db.Get(vaultsBucket, key, &Vault{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(vaultsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
