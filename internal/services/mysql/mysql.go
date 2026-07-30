// Package mysql implementa Microsoft.DBforMySQL/flexibleServers
// (ARM CRUD sync), con sub-recursos databases y firewallRules, y un stub
// de configurations para que azurerm_mysql_flexible_server no muestre drift.
package mysql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	serversBucket  = "mysql.flexibleservers"
	dbsBucket      = "mysql.databases"
	fwBucket       = "mysql.firewallrules"
	providerBase   = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DBforMySQL/flexibleServers"
	resourceType   = "Microsoft.DBforMySQL/flexibleServers"
	dbResourceType = "Microsoft.DBforMySQL/flexibleServers/databases"
	fwResourceType = "Microsoft.DBforMySQL/flexibleServers/firewallRules"
	fqdnSuffix     = ".mysql.database.azure.com"
)

// Service expone el registro de rutas para MySQL Flexible Server.
type Service struct{ db *storage.DB }

// New crea una instancia del servicio.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register registra todas las rutas ARM para MySQL Flexible Server.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+providerBase, s.listServers)
	mux.HandleFunc("PUT "+providerBase+"/{serverName}", s.putServer)
	mux.HandleFunc("GET "+providerBase+"/{serverName}", s.getServerHandler)
	mux.HandleFunc("DELETE "+providerBase+"/{serverName}", s.deleteServer)

	dbBase := providerBase + "/{serverName}/databases"
	mux.HandleFunc("GET "+dbBase, s.listDatabases)
	mux.HandleFunc("PUT "+dbBase+"/{dbName}", s.putDatabase)
	mux.HandleFunc("GET "+dbBase+"/{dbName}", s.getDatabase)
	mux.HandleFunc("DELETE "+dbBase+"/{dbName}", s.deleteDatabase)

	fwBase := providerBase + "/{serverName}/firewallRules"
	mux.HandleFunc("GET "+fwBase, s.listFirewallRules)
	mux.HandleFunc("PUT "+fwBase+"/{ruleName}", s.putFirewallRule)
	mux.HandleFunc("GET "+fwBase+"/{ruleName}", s.getFirewallRule)
	mux.HandleFunc("DELETE "+fwBase+"/{ruleName}", s.deleteFirewallRule)

	cfgBase := providerBase + "/{serverName}/configurations"
	mux.HandleFunc("GET "+cfgBase, s.listConfigurations)
	mux.HandleFunc("PUT "+cfgBase+"/{configName}", s.putConfiguration)
	mux.HandleFunc("GET "+cfgBase+"/{configName}", s.getConfiguration)
}

// ── Flexible Server ────────────────────────────────────────────────────────

// FlexibleServer replica el shape que azurerm_mysql_flexible_server lee.
// Para MySQL el sku.name es "Standard_B1ms" (sin prefijo de tier) y el tier
// viene en sku.tier; preservamos lo que el cliente envíe.
type FlexibleServer struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        FlexibleSku       `json:"sku"`
	Properties ServerProperties  `json:"properties"`
}

type FlexibleSku struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

type ServerProperties struct {
	AdministratorLogin       string           `json:"administratorLogin"`
	Version                  string           `json:"version"`
	State                    string           `json:"state"`
	FullyQualifiedDomainName string           `json:"fullyQualifiedDomainName"`
	Storage                  StorageProfile   `json:"storage"`
	Backup                   BackupProfile    `json:"backup"`
	HighAvailability         HighAvailability `json:"highAvailability"`
	ReplicaCapacity          int              `json:"replicaCapacity"`
	ReplicationRole          string           `json:"replicationRole"`
}

type StorageProfile struct {
	StorageSizeGB  int    `json:"storageSizeGB"`
	AutoGrow       string `json:"autoGrow"`
	AutoIoScaling  string `json:"autoIoScaling,omitempty"`
	Iops           int    `json:"iops,omitempty"`
}

type BackupProfile struct {
	BackupRetentionDays int    `json:"backupRetentionDays"`
	GeoRedundantBackup  string `json:"geoRedundantBackup"`
}

type HighAvailability struct {
	Mode string `json:"mode"`
}

type serverRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *FlexibleSku      `json:"sku,omitempty"`
	Properties struct {
		AdministratorLogin         string           `json:"administratorLogin"`
		AdministratorLoginPassword string           `json:"administratorLoginPassword"`
		Version                    string           `json:"version"`
		Storage                    *StorageProfile  `json:"storage,omitempty"`
		Backup                     *BackupProfile   `json:"backup,omitempty"`
		HighAvailability           *HighAvailability `json:"highAvailability,omitempty"`
	} `json:"properties"`
}

func serverKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func serverID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.DBforMySQL/flexibleServers/%s", sub, rg, name)
}

func defaultSkuTier(name string) string {
	switch {
	case strings.HasPrefix(name, "Standard_B"):
		return "Burstable"
	case strings.HasPrefix(name, "Standard_D"):
		return "GeneralPurpose"
	case strings.HasPrefix(name, "Standard_E"):
		return "MemoryOptimized"
	default:
		return "Burstable"
	}
}

func (s *Service) putServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")

	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := serverKey(sub, rg, name)
	existing, found, err := s.loadServer(key)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	adminLogin := req.Properties.AdministratorLogin
	if found && adminLogin == "" {
		adminLogin = existing.Properties.AdministratorLogin
	}

	version := req.Properties.Version
	if version == "" {
		version = "8.0.21"
	}

	var sku FlexibleSku
	if req.Sku != nil {
		sku = *req.Sku
	} else {
		sku = FlexibleSku{Name: "Standard_B1ms", Tier: "Burstable"}
	}
	if sku.Tier == "" {
		sku.Tier = defaultSkuTier(sku.Name)
	}

	stor := StorageProfile{StorageSizeGB: 20, AutoGrow: "Enabled"}
	if req.Properties.Storage != nil {
		if req.Properties.Storage.StorageSizeGB > 0 {
			stor.StorageSizeGB = req.Properties.Storage.StorageSizeGB
		}
		if req.Properties.Storage.AutoGrow != "" {
			stor.AutoGrow = req.Properties.Storage.AutoGrow
		}
		stor.Iops = req.Properties.Storage.Iops
		stor.AutoIoScaling = req.Properties.Storage.AutoIoScaling
	}

	backup := BackupProfile{BackupRetentionDays: 7, GeoRedundantBackup: "Disabled"}
	if req.Properties.Backup != nil {
		if req.Properties.Backup.BackupRetentionDays > 0 {
			backup.BackupRetentionDays = req.Properties.Backup.BackupRetentionDays
		}
		if req.Properties.Backup.GeoRedundantBackup != "" {
			backup.GeoRedundantBackup = req.Properties.Backup.GeoRedundantBackup
		}
	}

	ha := HighAvailability{Mode: "Disabled"}
	if req.Properties.HighAvailability != nil && req.Properties.HighAvailability.Mode != "" {
		ha.Mode = req.Properties.HighAvailability.Mode
	}

	srv := FlexibleServer{
		ID:       serverID(sub, rg, name),
		Name:     name,
		Type:     resourceType,
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: ServerProperties{
			AdministratorLogin:       adminLogin,
			Version:                  version,
			State:                    "Ready",
			FullyQualifiedDomainName: name + fqdnSuffix,
			Storage:                  stor,
			Backup:                   backup,
			HighAvailability:         ha,
			ReplicaCapacity:          5,
			ReplicationRole:          "None",
		},
	}

	if err := s.db.Put(serversBucket, key, srv); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, srv)
}

func (s *Service) loadServer(key string) (FlexibleServer, bool, error) {
	var srv FlexibleServer
	found, err := s.db.Get(serversBucket, key, &srv)
	return srv, found, err
}

func (s *Service) getServerHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")

	srv, found, err := s.loadServer(serverKey(sub, rg, name))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("MySQL flexible server '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, srv)
}

func (s *Service) listServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	items := make([]FlexibleServer, 0)
	_ = s.db.List(serversBucket, sub+"/"+rg+"/", func(_ string, raw []byte) error {
		var srv FlexibleServer
		if err := json.Unmarshal(raw, &srv); err == nil {
			items = append(items, srv)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")
	_ = s.db.Delete(serversBucket, serverKey(sub, rg, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Databases ──────────────────────────────────────────────────────────────

type MySQLDatabase struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Properties MySQLDatabaseProperties `json:"properties"`
}

type MySQLDatabaseProperties struct {
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
}

func dbKey(sub, rg, srv, name string) string { return sub + "/" + rg + "/" + srv + "/" + name }
func dbID(sub, rg, srv, name string) string  { return serverID(sub, rg, srv) + "/databases/" + name }

func (s *Service) putDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("dbName")

	var req struct {
		Properties MySQLDatabaseProperties `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Properties.Charset == "" {
		req.Properties.Charset = "utf8mb4"
	}
	if req.Properties.Collation == "" {
		req.Properties.Collation = "utf8mb4_unicode_ci"
	}

	key := dbKey(sub, rg, srvName, name)
	var existing MySQLDatabase
	found, _ := s.db.Get(dbsBucket, key, &existing)

	db := MySQLDatabase{
		ID:         dbID(sub, rg, srvName, name),
		Name:       name,
		Type:       dbResourceType,
		Properties: req.Properties,
	}
	if err := s.db.Put(dbsBucket, key, db); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, db)
}

func (s *Service) getDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("dbName")
	var db MySQLDatabase
	found, err := s.db.Get(dbsBucket, dbKey(sub, rg, srvName, name), &db)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("database '%s' no encontrada", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, db)
}

func (s *Service) listDatabases(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	items := make([]MySQLDatabase, 0)
	_ = s.db.List(dbsBucket, sub+"/"+rg+"/"+srvName+"/", func(_ string, raw []byte) error {
		var db MySQLDatabase
		if err := json.Unmarshal(raw, &db); err == nil {
			items = append(items, db)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("dbName")
	_ = s.db.Delete(dbsBucket, dbKey(sub, rg, srvName, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Firewall Rules ─────────────────────────────────────────────────────────

type MySQLFirewallRule struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Type       string                      `json:"type"`
	Properties MySQLFirewallRuleProperties `json:"properties"`
}

type MySQLFirewallRuleProperties struct {
	StartIPAddress string `json:"startIpAddress"`
	EndIPAddress   string `json:"endIpAddress"`
}

func fwKey(sub, rg, srv, name string) string { return sub + "/" + rg + "/" + srv + "/" + name }
func fwID(sub, rg, srv, name string) string  { return serverID(sub, rg, srv) + "/firewallRules/" + name }

func (s *Service) putFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("ruleName")

	var req struct {
		Properties MySQLFirewallRuleProperties `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	key := fwKey(sub, rg, srvName, name)
	var existing MySQLFirewallRule
	found, _ := s.db.Get(fwBucket, key, &existing)

	rule := MySQLFirewallRule{
		ID:         fwID(sub, rg, srvName, name),
		Name:       name,
		Type:       fwResourceType,
		Properties: req.Properties,
	}
	if err := s.db.Put(fwBucket, key, rule); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rule)
}

func (s *Service) getFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("ruleName")
	var rule MySQLFirewallRule
	found, err := s.db.Get(fwBucket, fwKey(sub, rg, srvName, name), &rule)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("firewall rule '%s' no encontrada", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, rule)
}

func (s *Service) listFirewallRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	items := make([]MySQLFirewallRule, 0)
	_ = s.db.List(fwBucket, sub+"/"+rg+"/"+srvName+"/", func(_ string, raw []byte) error {
		var rule MySQLFirewallRule
		if err := json.Unmarshal(raw, &rule); err == nil {
			items = append(items, rule)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	name := r.PathValue("ruleName")
	_ = s.db.Delete(fwBucket, fwKey(sub, rg, srvName, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Configurations (stub) ─────────────────────────────────────────────────

func (s *Service) listConfigurations(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

func (s *Service) getConfiguration(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	cfgName := r.PathValue("configName")
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   serverID(sub, rg, srvName) + "/configurations/" + cfgName,
		"name": cfgName,
		"type": "Microsoft.DBforMySQL/flexibleServers/configurations",
		"properties": map[string]any{
			"value":        "",
			"defaultValue": "",
			"dataType":     "String",
			"source":       "system-default",
		},
	})
}

func (s *Service) putConfiguration(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	cfgName := r.PathValue("configName")
	var req struct {
		Properties struct {
			Value string `json:"value"`
		} `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   serverID(sub, rg, srvName) + "/configurations/" + cfgName,
		"name": cfgName,
		"type": "Microsoft.DBforMySQL/flexibleServers/configurations",
		"properties": map[string]any{
			"value":        req.Properties.Value,
			"defaultValue": "",
			"dataType":     "String",
			"source":       "user-override",
		},
	})
}
