// Package cosmosdb emula el subconjunto de Microsoft.DocumentDB (Cosmos
// DB, SQL API) más usado por azurerm/az: accounts (ARM, control plane),
// SQL databases y containers (también ARM — igual que Service Bus,
// azurerm_cosmosdb_sql_database/azurerm_cosmosdb_sql_container son
// sub-recursos ARM anidados, no entidades de data-plane) y, por último,
// el data-plane real de documentos.
//
// Crear una cuenta de Cosmos DB es asíncrono y lento en Azure real
// (aprovisiona infraestructura multi-región), así que el PUT responde
// 202 + Azure-AsyncOperation, igual que storage accounts/Service Bus
// namespaces. Databases/containers, en cambio, son mutaciones rápidas y
// se modelan síncronas.
//
// El data-plane de documentos vive en
// https://{account}.documents.azure.com/dbs/{db}/colls/{container}/docs/...
// en Azure real, usando headers especiales (x-ms-documentdb-partitionkey,
// etc.) en vez de un body JSON simple. Aquí, siguiendo la misma
// simplificación pragmática que el resto del emulador (JSON liso en vez
// del protocolo real), se sirve bajo
// "/{account}.documents/dbs/{db}/colls/{container}/docs[/{id}]" vía el
// dispatcher compartido (ver cmd/azure-emulator/main.go), con PUT en vez
// de POST/headers especiales para crear/reemplazar un documento.
package cosmosdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const accountsBucket = "cosmosdb.accounts"

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.DocumentDB (accounts, sqlDatabases, containers) y el
// data-plane de documentos.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de Cosmos DB.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Locations replica "properties.locations[]" de una cuenta (lista de
// regiones); el emulador no modela multi-región real, solo guarda lo que
// el cliente mandó para que las lecturas posteriores lo reflejen.
type Location struct {
	LocationName string `json:"locationName"`
}

// Account replica la forma estándar de ARM para
// Microsoft.DocumentDB/databaseAccounts.
type Account struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Kind       string            `json:"kind"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties AccountProperties `json:"properties"`
}

// AccountProperties incluye el estado de aprovisionamiento y el endpoint
// de documentos, que es lo que az/Terraform leen después de crear la
// cuenta para saber a dónde apuntar el data-plane.
type AccountProperties struct {
	ProvisioningState string     `json:"provisioningState"`
	DocumentEndpoint  string     `json:"documentEndpoint"`
	Locations         []Location `json:"locations,omitempty"`
}

type accountRequest struct {
	Location   string            `json:"location"`
	Kind       string            `json:"kind"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		Locations []Location `json:"locations,omitempty"`
	} `json:"properties"`
}

// Register monta las rutas ARM de Microsoft.DocumentDB en mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts",
		s.listAccounts)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}",
		s.putAccount)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}",
		s.getAccount)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}",
		s.deleteAccount)

	s.registerDatabases(mux)
	s.registerContainers(mux)
}

func accountKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func accountID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.DocumentDB/databaseAccounts/%s", subID, rg, name)
}

func documentEndpoint(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.documents/", scheme, r.Host, name)
}

func (s *Service) putAccount(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var req accountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio para crear una cuenta de Cosmos DB")
		return
	}
	if strings.TrimSpace(req.Kind) == "" {
		req.Kind = "GlobalDocumentDB"
	}
	locations := req.Properties.Locations
	if len(locations) == 0 {
		locations = []Location{{LocationName: req.Location}}
	}

	key := accountKey(subID, rg, name)
	acct := Account{
		ID:       accountID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.DocumentDB/databaseAccounts",
		Location: req.Location,
		Kind:     req.Kind,
		Tags:     req.Tags,
		Properties: AccountProperties{
			ProvisioningState: "Succeeded",
			DocumentEndpoint:  documentEndpoint(r, name),
			Locations:         locations,
		},
	}

	if err := s.db.Put(accountsBucket, key, acct); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Igual que storage accounts/Service Bus namespaces: crear una cuenta
	// de Cosmos DB es asíncrono (y lento) en Azure real.
	w.Header().Set("Content-Type", "application/json")
	id := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, "Microsoft.DocumentDB", req.Location, id, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, acct)
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	acct, found, err := s.getAccountRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la cuenta de Cosmos DB '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, acct)
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	accounts := make([]Account, 0)
	err := s.db.List(accountsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var acct Account
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

// deleteAccount es idempotente (204 si no existe). No borra en cascada
// databases/containers (igual de simplificado que el resto del emulador
// con recursos padre/hijo).
func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")
	key := accountKey(subID, rg, name)

	found, err := s.db.Get(accountsBucket, key, &Account{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(accountsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getAccountRecord es el helper interno (usado también por databases.go
// para validar que la cuenta padre existe) que busca una cuenta por
// subID/rg/name.
func (s *Service) getAccountRecord(subID, rg, name string) (Account, bool, error) {
	var acct Account
	found, err := s.db.Get(accountsBucket, accountKey(subID, rg, name), &acct)
	return acct, found, err
}
