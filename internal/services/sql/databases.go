package sql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const databasesBucket = "sql.databases"

// Database replica el subconjunto relevante de
// Microsoft.Sql/servers/databases que az/Terraform (azurerm_mssql_database)
// leen. No hay motor de consultas real: es un registro ARM con propiedades
// fake -- "shape-compatible, no behavior-complete" como el resto de los
// data planes simplificados de este proyecto.
type Database struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Location   string             `json:"location"`
	Tags       map[string]string  `json:"tags,omitempty"`
	Sku        *DatabaseSku       `json:"sku,omitempty"`
	Properties DatabaseProperties `json:"properties"`
}

type DatabaseSku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

type DatabaseProperties struct {
	Collation    string `json:"collation,omitempty"`
	MaxSizeBytes int64  `json:"maxSizeBytes,omitempty"`
	Status       string `json:"status"`
	CreationDate string `json:"creationDate,omitempty"`
	// CurrentServiceObjectiveName es lo que el Read de azurerm_mssql_database
	// realmente lee para popular sku_name (mssql_database_resource.go:
	// "skuName = *props.CurrentServiceObjectiveName") -- NO el "sku.name" de
	// nivel superior como se asumió originalmente. Sin este campo, el
	// provider siempre ve sku_name como ausente y muestra un diff falso
	// "+ sku_name" en cada plan/apply.
	CurrentServiceObjectiveName string `json:"currentServiceObjectiveName,omitempty"`
	// RequestedBackupStorageRedundancy es lo que el Read realmente lee para
	// storage_account_type ("BackupStorageRedundancy =
	// string(*props.RequestedBackupStorageRedundancy)") -- no
	// "currentBackupStorageRedundancy" como se asumió originalmente. Se
	// mantienen ambos campos en la respuesta por si una versión distinta del
	// provider lee el otro.
	RequestedBackupStorageRedundancy string `json:"requestedBackupStorageRedundancy,omitempty"`
	CurrentBackupStorageRedundancy   string `json:"currentBackupStorageRedundancy,omitempty"`
}

type databaseRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *DatabaseSku      `json:"sku,omitempty"`
	Properties struct {
		Collation                        string `json:"collation"`
		MaxSizeBytes                     int64  `json:"maxSizeBytes"`
		RequestedBackupStorageRedundancy string `json:"requestedBackupStorageRedundancy"`
	} `json:"properties"`
}

func databaseKey(subID, rg, srvName, dbName string) string {
	return subID + "/" + rg + "/" + srvName + "/" + dbName
}

func (s *Service) registerDatabases(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers/{serverName}/databases"
	mux.HandleFunc("GET "+base, s.listDatabases)
	mux.HandleFunc("PUT "+base+"/{databaseName}", s.putDatabase)
	// El provider real usa PATCH (no PUT) para actualizaciones in-place de
	// una database existente (p.ej. cambiar sku_name/storage_account_type
	// sin recrearla) -- mismo handler, putDatabase ya es idempotente y
	// preserva creationDate cuando existedBefore es true.
	mux.HandleFunc("PATCH "+base+"/{databaseName}", s.putDatabase)
	mux.HandleFunc("GET "+base+"/{databaseName}", s.getDatabaseHandler)
	mux.HandleFunc("DELETE "+base+"/{databaseName}", s.deleteDatabase)

	// restorableDroppedDatabases: azurerm_mssql_server lo consulta durante
	// cada refresh/plan (parte de su Read, no algo que el usuario pida
	// explícitamente) para poblar metadata de point-in-time-restore. No
	// hay backups reales en este emulador -- siempre se responde una lista
	// vacía, "shape-compatible, no behavior-complete" como el resto del
	// servicio.
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers/{serverName}/restorableDroppedDatabases", s.listRestorableDroppedDatabases)
}

func (s *Service) listRestorableDroppedDatabases(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

// putDatabase es síncrono, sub-recurso anidado de un solo nivel -- mismo
// patrón que eventhub.putHub: valida que el server padre exista antes de
// crear/actualizar.
func (s *Service) putDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")

	srv, found, err := s.getServer(subID, rg, srvName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", srvName, rg))
		return
	}

	var req databaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	key := databaseKey(subID, rg, srvName, dbName)
	existing, existedBefore, err := s.getDatabase(subID, rg, srvName, dbName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	collation := req.Properties.Collation
	if collation == "" {
		collation = "SQL_Latin1_General_CP1_CI_AS"
	}
	maxSize := req.Properties.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = 2147483648 // 2 GiB, el default de la tier "Basic"
	}
	creationDate := time.Now().UTC().Format(time.RFC3339)
	if existedBefore {
		creationDate = existing.Properties.CreationDate
	}

	location := req.Location
	if location == "" {
		location = srv.Location
	}

	// storageRedundancy preserva el valor existente en un PATCH parcial que
	// no lo incluye (igual que collation/maxSize), y cae al default real de
	// Azure ("Geo") cuando nunca se especificó.
	storageRedundancy := req.Properties.RequestedBackupStorageRedundancy
	if storageRedundancy == "" {
		if existedBefore && existing.Properties.CurrentBackupStorageRedundancy != "" {
			storageRedundancy = existing.Properties.CurrentBackupStorageRedundancy
		} else {
			storageRedundancy = "Geo"
		}
	}

	sku := req.Sku
	if sku == nil && existedBefore {
		sku = existing.Sku
	}
	// serviceObjective es lo que el Read real lee para sku_name
	// ("skuName = *props.CurrentServiceObjectiveName", no el "sku.name" de
	// nivel superior). Se deriva del mismo sku para no tener dos fuentes de
	// verdad divergentes.
	serviceObjective := ""
	if sku != nil {
		serviceObjective = sku.Name
	}

	db := Database{
		ID:       srv.ID + "/databases/" + dbName,
		Name:     dbName,
		Type:     "Microsoft.Sql/servers/databases",
		Location: location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: DatabaseProperties{
			Collation:                        collation,
			MaxSizeBytes:                     maxSize,
			Status:                           "Online",
			CreationDate:                     creationDate,
			CurrentServiceObjectiveName:      serviceObjective,
			RequestedBackupStorageRedundancy: storageRedundancy,
			CurrentBackupStorageRedundancy:   storageRedundancy,
		},
	}
	if err := s.db.Put(databasesBucket, key, db); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, db)
}

func (s *Service) getDatabase(subID, rg, srvName, dbName string) (Database, bool, error) {
	var db Database
	found, err := s.db.Get(databasesBucket, databaseKey(subID, rg, srvName, dbName), &db)
	return db, found, err
}

func (s *Service) getDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")

	db, found, err := s.getDatabase(subID, rg, srvName, dbName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la database '%s' no existe en el server '%s'", dbName, srvName))
		return
	}
	server.WriteJSON(w, http.StatusOK, db)
}

func (s *Service) listDatabases(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")

	dbs := make([]Database, 0)
	err := s.db.List(databasesBucket, subID+"/"+rg+"/"+srvName+"/", func(_ string, raw []byte) error {
		var db Database
		if err := json.Unmarshal(raw, &db); err != nil {
			return err
		}
		dbs = append(dbs, db)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": dbs})
}

func (s *Service) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")
	key := databaseKey(subID, rg, srvName, dbName)

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
