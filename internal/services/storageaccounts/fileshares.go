package storageaccounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const fileShareBucket = "storage.fileShares"

// FileServiceProperties son las propiedades del servicio de archivos (el
// nodo raíz "default" que agrupa todos los shares de una cuenta).
// El provider azurerm lee este recurso cuando hace GET .../fileServices/default.
type FileServiceProperties struct {
	ShareDeleteRetentionPolicy ShareRetentionPolicy `json:"shareDeleteRetentionPolicy"`
}

// ShareRetentionPolicy indica si el borrado suave está habilitado.
type ShareRetentionPolicy struct {
	Enabled bool `json:"enabled"`
	Days    *int `json:"days"`
}

// FileServiceResource es la respuesta ARM de .../fileServices/default.
type FileServiceResource struct {
	ID         string                `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Properties FileServiceProperties `json:"properties"`
}

// FileShare es la representación ARM de
// Microsoft.Storage/storageAccounts/fileServices/shares.
type FileShare struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Etag       string            `json:"etag,omitempty"`
	Properties FileShareProperties `json:"properties"`
}

// FileShareProperties son los campos que lee azurerm_storage_share en Read.
type FileShareProperties struct {
	ShareQuota       int               `json:"shareQuota"`
	EnabledProtocols string            `json:"enabledProtocols,omitempty"`
	AccessTier       string            `json:"accessTier,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	LastModifiedTime string            `json:"lastModifiedTime,omitempty"`
}

// fileShareRequest es el body del PUT que manda el provider.
type fileShareRequest struct {
	Properties struct {
		ShareQuota       int               `json:"shareQuota"`
		EnabledProtocols string            `json:"enabledProtocols,omitempty"`
		AccessTier       string            `json:"accessTier,omitempty"`
		Metadata         map[string]string `json:"metadata,omitempty"`
	} `json:"properties"`
}

func fileShareKey(subID, rg, account, share string) string {
	return subID + "/" + rg + "/" + account + "/" + share
}

func fileShareKeyPrefix(subID, rg, account string) string {
	return subID + "/" + rg + "/" + account + "/"
}

func fileServiceID(subID, rg, account string) string {
	return "/subscriptions/" + subID +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.Storage/storageAccounts/" + account +
		"/fileServices/default"
}

func fileShareID(subID, rg, account, share string) string {
	return "/subscriptions/" + subID +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.Storage/storageAccounts/" + account +
		"/fileServices/default/shares/" + share
}

// registerFileShares monta las rutas del file service + shares en mux.
// Llamado desde storageaccounts.Register() para mantener todo en el mismo
// paquete sin agregar un service container separado.
func (s *Service) registerFileShares(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}" +
		"/providers/Microsoft.Storage/storageAccounts/{accountName}"

	// fileServices (recurso contenedor)
	mux.HandleFunc("GET "+base+"/fileServices", s.listFileServices)
	mux.HandleFunc("GET "+base+"/fileServices/default", s.getFileService)
	mux.HandleFunc("PUT "+base+"/fileServices/default", s.putFileService)

	// shares (sub-recurso)
	mux.HandleFunc("GET "+base+"/fileServices/default/shares", s.listFileShares)
	mux.HandleFunc("PUT "+base+"/fileServices/default/shares/{shareName}", s.putFileShare)
	mux.HandleFunc("GET "+base+"/fileServices/default/shares/{shareName}", s.getFileShareHandler)
	mux.HandleFunc("DELETE "+base+"/fileServices/default/shares/{shareName}", s.deleteFileShare)
}

// ---------- fileServices handlers ----------

func (s *Service) listFileServices(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")

	svc := s.defaultFileService(subID, rg, account)
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []FileServiceResource{svc}})
}

func (s *Service) getFileService(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")

	server.WriteJSON(w, http.StatusOK, s.defaultFileService(subID, rg, account))
}

// putFileService acepta cambios de propiedades del file service (p. ej.
// configurar retención de borrado suave), pero en el emulador simplemente
// devuelve el recurso con los valores por defecto sin persistir nada.
func (s *Service) putFileService(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")

	server.WriteJSON(w, http.StatusOK, s.defaultFileService(subID, rg, account))
}

func (s *Service) defaultFileService(subID, rg, account string) FileServiceResource {
	return FileServiceResource{
		ID:   fileServiceID(subID, rg, account),
		Name: "default",
		Type: "Microsoft.Storage/storageAccounts/fileServices",
		Properties: FileServiceProperties{
			ShareDeleteRetentionPolicy: ShareRetentionPolicy{Enabled: false},
		},
	}
}

// ---------- shares handlers ----------

func (s *Service) putFileShare(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")
	shareName := r.PathValue("shareName")

	var req fileShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	// defaults
	quota := req.Properties.ShareQuota
	if quota <= 0 {
		quota = 5120 // 5 TiB — Azure default
	}
	proto := req.Properties.EnabledProtocols
	if proto == "" {
		proto = "SMB"
	}
	tier := req.Properties.AccessTier
	if tier == "" {
		tier = "TransactionOptimized"
	}
	metadata := req.Properties.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}

	key := fileShareKey(subID, rg, account, shareName)
	var existing FileShare
	existedBefore, err := s.db.Get(fileShareBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	share := FileShare{
		ID:   fileShareID(subID, rg, account, shareName),
		Name: shareName,
		Type: "Microsoft.Storage/storageAccounts/fileServices/shares",
		Etag: "\"0x0000000000000001\"",
		Properties: FileShareProperties{
			ShareQuota:       quota,
			EnabledProtocols: proto,
			AccessTier:       tier,
			Metadata:         metadata,
			LastModifiedTime: time.Now().UTC().Format(time.RFC3339),
		},
	}

	if err := s.db.Put(fileShareBucket, key, share); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, share)
}

func (s *Service) getFileShare(subID, rg, account, name string) (FileShare, bool, error) {
	var sh FileShare
	found, err := s.db.Get(fileShareBucket, fileShareKey(subID, rg, account, name), &sh)
	return sh, found, err
}

func (s *Service) getFileShareHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")
	shareName := r.PathValue("shareName")

	sh, found, err := s.getFileShare(subID, rg, account, shareName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ShareNotFound",
			fmt.Sprintf("el file share '%s' no existe en la cuenta '%s'", shareName, account))
		return
	}
	server.WriteJSON(w, http.StatusOK, sh)
}

func (s *Service) listFileShares(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")

	shares := make([]FileShare, 0)
	err := s.db.List(fileShareBucket, fileShareKeyPrefix(subID, rg, account), func(_ string, raw []byte) error {
		var sh FileShare
		if err := json.Unmarshal(raw, &sh); err != nil {
			return err
		}
		shares = append(shares, sh)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": shares})
}

func (s *Service) deleteFileShare(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	account := r.PathValue("accountName")
	shareName := r.PathValue("shareName")
	key := fileShareKey(subID, rg, account, shareName)

	found, err := s.db.Get(fileShareBucket, key, &FileShare{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(fileShareBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
