package cosmosdb

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const containersBucket = "cosmosdb.containers"

// Container replica la forma estándar de ARM para
// Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers.
type Container struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Properties ContainerProperties `json:"properties"`
}

type ContainerProperties struct {
	Resource ContainerResource `json:"resource"`
}

// ContainerResource incluye partitionKey, que es obligatorio en Azure
// real para crear un container de SQL API.
type ContainerResource struct {
	ID           string       `json:"id"`
	PartitionKey PartitionKey `json:"partitionKey"`
}

type PartitionKey struct {
	Paths   []string `json:"paths"`
	Kind    string   `json:"kind,omitempty"`
	Version int      `json:"version,omitempty"`
}

type containerRequest struct {
	Properties struct {
		Resource ContainerResource `json:"resource"`
	} `json:"properties"`
}

func (s *Service) registerContainers(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}/containers",
		s.listContainers)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}/containers/{containerName}",
		s.putContainer)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}/containers/{containerName}",
		s.getContainer)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}/containers/{containerName}",
		s.deleteContainer)
}

func containerKey(subID, rg, account, db, container string) string {
	return subID + "/" + rg + "/" + account + "/" + db + "/" + container
}

func (s *Service) putContainer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")
	containerName := r.PathValue("containerName")

	db, found, err := s.getDatabaseRecord(subID, rg, accountName, dbName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la base de datos '%s' no existe en la cuenta '%s'", dbName, accountName))
		return
	}

	var req containerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if req.Properties.Resource.ID == "" {
		req.Properties.Resource.ID = containerName
	}
	if len(req.Properties.Resource.PartitionKey.Paths) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.resource.partitionKey.paths' es obligatorio para crear un container")
		return
	}

	key := containerKey(subID, rg, accountName, dbName, containerName)
	_, existedBefore, err := s.getContainerRecord(subID, rg, accountName, dbName, containerName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	c := Container{
		ID:   db.ID + "/containers/" + containerName,
		Name: containerName,
		Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers",
		Properties: ContainerProperties{
			Resource: req.Properties.Resource,
		},
	}
	if err := s.db.Put(containersBucket, key, c); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneContainer(containerEntityPath(accountName, dbName, containerName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, c)
}

func (s *Service) getContainer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")
	containerName := r.PathValue("containerName")

	c, found, err := s.getContainerRecord(subID, rg, accountName, dbName, containerName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el container '%s' no existe en la base de datos '%s'", containerName, dbName))
		return
	}
	server.WriteJSON(w, http.StatusOK, c)
}

func (s *Service) listContainers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")

	containers := make([]Container, 0)
	err := s.db.List(containersBucket, subID+"/"+rg+"/"+accountName+"/"+dbName+"/", func(key string, raw []byte) error {
		var c Container
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		containers = append(containers, c)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": containers})
}

// deleteContainer es idempotente (204 si no existe). No borra en cascada
// los documentos del data-plane (igual de simplificado que el resto del
// emulador con recursos padre/hijo).
func (s *Service) deleteContainer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")
	containerName := r.PathValue("containerName")
	key := containerKey(subID, rg, accountName, dbName, containerName)

	found, err := s.db.Get(containersBucket, key, &Container{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(containersBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.unmarkDataPlaneContainer(containerEntityPath(accountName, dbName, containerName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getContainerRecord es el helper interno que busca un container ARM por
// subID/rg/account/db/name.
func (s *Service) getContainerRecord(subID, rg, account, db, name string) (Container, bool, error) {
	var c Container
	found, err := s.db.Get(containersBucket, containerKey(subID, rg, account, db, name), &c)
	return c, found, err
}
