// Package recoveryservices emula Microsoft.RecoveryServices/vaults: ARM CRUD
// síncrono más los sub-recursos read-only que azurerm_recovery_services_vault
// lee en cada Refresh (backupConfig, backupPolicies, backupStorageConfig).
// No hay backup real detrás; todo es shape-compatible.
package recoveryservices

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const vaultsBucket = "recoveryservices.vaults"

// Service agrupa el estado necesario para las rutas de Recovery Services.
type Service struct {
	db *storage.DB
}

// New crea el servicio de Recovery Services.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register monta todas las rutas en mux.
func (s *Service) Register(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}" +
		"/providers/Microsoft.RecoveryServices/vaults"

	mux.HandleFunc("GET "+base, s.listVaults)
	mux.HandleFunc("PUT "+base+"/{vaultName}", s.putVault)
	mux.HandleFunc("GET "+base+"/{vaultName}", s.getVaultHandler)
	mux.HandleFunc("DELETE "+base+"/{vaultName}", s.deleteVault)

	// Sub-recursos read-only que azurerm_recovery_services_vault lee en Read.
	mux.HandleFunc("GET "+base+"/{vaultName}/backupConfig/vaultconfig", s.getBackupConfig)
	mux.HandleFunc("PUT "+base+"/{vaultName}/backupConfig/vaultconfig", s.putBackupConfig)
	mux.HandleFunc("GET "+base+"/{vaultName}/backupPolicies", s.listBackupPolicies)
	mux.HandleFunc("GET "+base+"/{vaultName}/backupStorageConfig/vaultStorageConfig", s.getBackupStorageConfig)
	mux.HandleFunc("PUT "+base+"/{vaultName}/backupStorageConfig/vaultStorageConfig", s.putBackupStorageConfig)
}

// ---------- tipos ARM ----------

// Vault es la representación ARM de Microsoft.RecoveryServices/vaults.
type Vault struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Location   string           `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        VaultSku         `json:"sku"`
	Properties VaultProperties  `json:"properties"`
}

// VaultSku replica {"name":"Standard"} o {"name":"RS0","tier":"Standard"}.
type VaultSku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// VaultProperties son los campos que azurerm lee después de crear el vault.
type VaultProperties struct {
	ProvisioningState   string `json:"provisioningState"`
	PublicNetworkAccess string `json:"publicNetworkAccess,omitempty"`
}

type vaultRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *VaultSku         `json:"sku,omitempty"`
	Properties struct {
		PublicNetworkAccess string `json:"publicNetworkAccess,omitempty"`
	} `json:"properties,omitempty"`
}

// ---------- helpers ----------

func vaultKey(subID, rg, name string) string    { return subID + "/" + rg + "/" + name }
func vaultKeyPrefix(subID, rg string) string     { return subID + "/" + rg + "/" }

func vaultID(subID, rg, name string) string {
	return "/subscriptions/" + subID +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.RecoveryServices/vaults/" + name
}

func (s *Service) getVault(subID, rg, name string) (Vault, bool, error) {
	var v Vault
	found, err := s.db.Get(vaultsBucket, vaultKey(subID, rg, name), &v)
	return v, found, err
}

// ---------- vault handlers ----------

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

	sku := VaultSku{Name: "Standard"}
	if req.Sku != nil && req.Sku.Name != "" {
		sku = *req.Sku
	}

	pna := req.Properties.PublicNetworkAccess
	if pna == "" {
		pna = "Enabled"
	}

	key := vaultKey(subID, rg, name)
	var existing Vault
	existedBefore, err := s.db.Get(vaultsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	vault := Vault{
		ID:       vaultID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.RecoveryServices/vaults",
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: VaultProperties{
			ProvisioningState:   "Succeeded",
			PublicNetworkAccess: pna,
		},
	}

	if err := s.db.Put(vaultsBucket, key, vault); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, vault)
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
	err := s.db.List(vaultsBucket, vaultKeyPrefix(subID, rg), func(_ string, raw []byte) error {
		var v Vault
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		vaults = append(vaults, v)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": vaults})
}

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
	w.WriteHeader(http.StatusNoContent)
}

// ---------- sub-recursos read-only ----------

// backupConfigBody es la respuesta fija de GET .../backupConfig/vaultconfig.
// El provider azurerm_recovery_services_vault lo lee en cada Refresh;
// sin este endpoint devuelve 404 y muestra un error en el plan.
func backupConfigBody(subID, rg, name string) map[string]any {
	return map[string]any{
		"id": vaultID(subID, rg, name) + "/backupConfig/vaultconfig",
		"name": "vaultconfig",
		"type": "Microsoft.RecoveryServices/vaults/backupConfig",
		"properties": map[string]any{
			"storageModelType":             "GeoRedundant",
			"storageType":                  "GeoRedundant",
			"storageTypeState":             "Locked",
			"enhancedSecurityState":        "Enabled",
			"softDeleteFeatureState":       "Enabled",
			"softDeleteRetentionPeriodDays": 14,
			"isSoftDeleteFeatureStateEditable": true,
		},
	}
}

func (s *Service) getBackupConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")
	server.WriteJSON(w, http.StatusOK, backupConfigBody(subID, rg, name))
}

func (s *Service) putBackupConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")
	// Aceptar el PUT sin persistir nada; devolver el shape fijo.
	server.WriteJSON(w, http.StatusOK, backupConfigBody(subID, rg, name))
}

// getBackupPolicies devuelve siempre una lista vacía.
// azurerm_recovery_services_vault hace GET a este endpoint en Read.
func (s *Service) listBackupPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

// backupStorageConfigBody es la respuesta fija de
// GET .../backupStorageConfig/vaultStorageConfig.
func backupStorageConfigBody(subID, rg, name string) map[string]any {
	return map[string]any{
		"id":   vaultID(subID, rg, name) + "/backupStorageConfig/vaultStorageConfig",
		"name": "vaultStorageConfig",
		"type": "Microsoft.RecoveryServices/vaults/backupStorageConfig",
		"properties": map[string]any{
			"storageModelType":       "GeoRedundant",
			"storageType":            "GeoRedundant",
			"storageTypeState":       "Locked",
			"crossRegionRestoreFlag": false,
			"dedupState":             "Disabled",
			"xcoolState":             "Disabled",
		},
	}
}

func (s *Service) getBackupStorageConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")
	server.WriteJSON(w, http.StatusOK, backupStorageConfigBody(subID, rg, name))
}

func (s *Service) putBackupStorageConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vaultName")
	server.WriteJSON(w, http.StatusOK, backupStorageConfigBody(subID, rg, name))
}
