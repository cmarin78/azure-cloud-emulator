// Package storageaccounts emula el nivel ARM (control plane) de
// Microsoft.Storage/storageAccounts: crear, leer, listar y borrar cuentas
// de almacenamiento dentro de un resource group.
//
// Esto es deliberadamente solo el control plane. El data plane (blobs,
// colas, tablas — lo que en Azure real vive en
// https://{account}.blob.core.windows.net/...) tiene un protocolo y un
// esquema de direccionamiento completamente distintos (sin "providers" en
// la URL, respuestas a veces en XML, headers de autenticación propios) y
// se modela en un paquete separado más adelante (ver ROADMAP.md, Fase 3).
//
// La creación real de una cuenta de almacenamiento en Azure es asíncrona
// (puede tardar mientras se aprovisiona infraestructura interna), así que
// PUT responde 202 + Azure-AsyncOperation, igual que el borrado de
// resource groups en internal/services/resourcemanager.
package storageaccounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const storageAccountsBucket = "storage.storageaccounts"

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Storage/storageAccounts.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de storage accounts.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Sku replica la forma mínima de "sku" que usa ARM para storage accounts
// (p. ej. {"name": "Standard_LRS"}).
type Sku struct {
	Name string `json:"name"`
}

// StorageAccount replica la forma estándar de ARM para
// Microsoft.Storage/storageAccounts.
type StorageAccount struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Sku        Sku                      `json:"sku"`
	Kind       string                   `json:"kind"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Properties StorageAccountProperties `json:"properties"`
}

// StorageAccountProperties incluye el estado de aprovisionamiento y los
// endpoints primarios (blob/queue/table/file), que es lo que la mayoría
// de las herramientas (az, Terraform, SDKs) leen después de crear la
// cuenta para saber a dónde apuntar el data plane.
type StorageAccountProperties struct {
	ProvisioningState string                    `json:"provisioningState"`
	PrimaryEndpoints  StorageAccountEndpoints   `json:"primaryEndpoints"`
}

// StorageAccountEndpoints apunta de vuelta al propio emulador: como no hay
// un host real por cuenta, todos los endpoints son la misma dirección base
// del emulador con el nombre de cuenta como primer segmento del path
// (estilo "path-style", igual que Azurite).
type StorageAccountEndpoints struct {
	Blob  string `json:"blob"`
	Queue string `json:"queue"`
	Table string `json:"table"`
	File  string `json:"file"`
}

type storageAccountRequest struct {
	Location string            `json:"location"`
	Sku      Sku               `json:"sku"`
	Kind     string            `json:"kind"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Register monta las rutas de storage accounts en mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts",
		s.listStorageAccounts)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts/{accountName}",
		s.putStorageAccount)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts/{accountName}",
		s.getStorageAccount)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts/{accountName}",
		s.deleteStorageAccount)
}

func storageAccountKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func endpoints(r *http.Request, accountName string) StorageAccountEndpoints {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s/%s", scheme, r.Host, accountName)
	return StorageAccountEndpoints{
		Blob:  base + ".blob/",
		Queue: base + ".queue/",
		Table: base + ".table/",
		File:  base + ".file/",
	}
}

func (s *Service) putStorageAccount(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var req storageAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio para crear una cuenta de almacenamiento")
		return
	}
	if strings.TrimSpace(req.Sku.Name) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'sku.name' es obligatorio (p. ej. 'Standard_LRS')")
		return
	}
	if strings.TrimSpace(req.Kind) == "" {
		req.Kind = "StorageV2"
	}

	key := storageAccountKey(subID, rg, name)

	acct := StorageAccount{
		ID:       fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Storage/storageAccounts",
		Location: req.Location,
		Sku:      req.Sku,
		Kind:     req.Kind,
		Tags:     req.Tags,
		Properties: StorageAccountProperties{
			ProvisioningState: "Succeeded",
			PrimaryEndpoints:  endpoints(r, name),
		},
	}

	if err := s.db.Put(storageAccountsBucket, key, acct); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// La creación real de una storage account es asíncrona: devolvemos
	// 202 + Azure-AsyncOperation. El cuerpo de la respuesta inmediata ya
	// incluye el recurso con provisioningState "Succeeded" porque, a
	// diferencia de Azure real, el emulador no necesita tiempo real de
	// aprovisionamiento — el polling es solo para que clientes que sí
	// esperan a Succeeded (az, Terraform) tengan algo válido para leer.
	w.Header().Set("Content-Type", "application/json")
	id := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, "Microsoft.Storage", req.Location, id, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, acct)
}

func (s *Service) getStorageAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var acct StorageAccount
	found, err := s.db.Get(storageAccountsBucket, storageAccountKey(subID, rg, name), &acct)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la storage account '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, acct)
}

func (s *Service) listStorageAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	accounts := make([]StorageAccount, 0)
	err := s.db.List(storageAccountsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var acct StorageAccount
		if err := json.Unmarshal(raw, &acct); err != nil {
			return err
		}
		accounts = append(accounts, acct)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": accounts})
}

// deleteStorageAccount es idempotente (204 si no existe) y, a diferencia
// del borrado de resource groups, en Azure real es síncrono: el comando
// vuelve cuando la cuenta ya fue borrada, sin polling.
func (s *Service) deleteStorageAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")
	key := storageAccountKey(subID, rg, name)

	found, err := s.db.Get(storageAccountsBucket, key, &StorageAccount{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(storageAccountsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
