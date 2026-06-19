package cosmosdb

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const databasesBucket = "cosmosdb.databases"

// Database replica la forma estándar de ARM para
// Microsoft.DocumentDB/databaseAccounts/sqlDatabases.
type Database struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Properties DatabaseProperties `json:"properties"`
}

type DatabaseProperties struct {
	Resource DatabaseResource `json:"resource"`
}

type DatabaseResource struct {
	ID string `json:"id"`
}

type databaseRequest struct {
	Properties struct {
		Resource DatabaseResource `json:"resource"`
	} `json:"properties"`
}

func (s *Service) registerDatabases(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases",
		s.listDatabases)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}",
		s.putDatabase)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}",
		s.getDatabase)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts/{accountName}/sqlDatabases/{databaseName}",
		s.deleteDatabase)
}

func databaseKey(subID, rg, account, db string) string {
	return subID + "/" + rg + "/" + account + "/" + db
}

func (s *Service) putDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")

	acct, found, err := s.getAccountRecord(subID, rg, accountName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la cuenta de Cosmos DB '%s' no existe en el resource group '%s'", accountName, rg))
		return
	}

	var req databaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if req.Properties.Resource.ID == "" {
		req.Properties.Resource.ID = dbName
	}

	key := databaseKey(subID, rg, accountName, dbName)
	_, existedBefore, err := s.getDatabaseRecord(subID, rg, accountName, dbName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	d := Database{
		ID:   acct.ID + "/sqlDatabases/" + dbName,
		Name: dbName,
		Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases",
		Properties: DatabaseProperties{
			Resource: req.Properties.Resource,
		},
	}
	if err := s.db.Put(databasesBucket, key, d); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, d)
}

func (s *Service) getDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")

	d, found, err := s.getDatabaseRecord(subID, rg, accountName, dbName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la base de datos '%s' no existe en la cuenta '%s'", dbName, accountName))
		return
	}
	server.WriteJSON(w, http.StatusOK, d)
}

func (s *Service) listDatabases(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")

	databases := make([]Database, 0)
	err := s.db.List(databasesBucket, subID+"/"+rg+"/"+accountName+"/", func(key string, raw []byte) error {
		var d Database
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		databases = append(databases, d)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": databases})
}

// deleteDatabase es idempotente (204 si no existe). No borra en cascada
// los containers anidados (igual de simplificado que el resto del
// emulador con recursos padre/hijo).
func (s *Service) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	dbName := r.PathValue("databaseName")
	key := databaseKey(subID, rg, accountName, dbName)

	found, err := s.db.Get(databasesBucket, key, &Database{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(databasesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getDatabaseRecord es el helper interno (usado también por
// containers.go para validar que la database padre existe).
func (s *Service) getDatabaseRecord(subID, rg, account, name string) (Database, bool, error) {
	var d Database
	found, err := s.db.Get(databasesBucket, databaseKey(subID, rg, account, name), &d)
	return d, found, err
}
