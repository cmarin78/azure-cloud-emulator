// Package postgresql implementa Microsoft.DBforPostgreSQL/flexibleServers
// (ARM CRUD sync), con sub-recursos databases y firewallRules, y un stub
// de configurations que devuelve lista vacía / valor predeterminado para que
// azurerm_postgresql_flexible_server no muestre drift en terraform plan.
package postgresql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	serversBucket   = "postgresql.flexibleservers"
	dbsBucket       = "postgresql.databases"
	fwBucket        = "postgresql.firewallrules"
	providerBase    = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DBforPostgreSQL/flexibleServers"
	resourceType    = "Microsoft.DBforPostgreSQL/flexibleServers"
	dbResourceType  = "Microsoft.DBforPostgreSQL/flexibleServers/databases"
	fwResourceType  = "Microsoft.DBforPostgreSQL/flexibleServers/firewallRules"
	fqdnSuffix      = ".postgres.database.azure.com"
)

// Service expone el registro de rutas para el emulador de PostgreSQL
// Flexible Server.
type Service struct{ db *storage.DB }

// New crea una instancia del servicio.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register registra todas las rutas de ARM para este servicio.
func (s *Service) Register(mux *http.ServeMux) {
	// Flexible server CRUD
	mux.HandleFunc("GET "+providerBase, s.listServers)
	mux.HandleFunc("PUT "+providerBase+"/{serverName}", s.putServer)
	mux.HandleFunc("GET "+providerBase+"/{serverName}", s.getServerHandler)
	mux.HandleFunc("DELETE "+providerBase+"/{serverName}", s.deleteServer)
	// Databases
	dbBase := providerBase + "/{serverName}/databases"
	mux.HandleFunc("GET "+dbBase, s.listDatabases)
	mux.HandleFunc("PUT "+dbBase+"/{dbName}", s.putDatabase)
	mux.HandleFunc("GET "+dbBase+"/{dbName}", s.getDatabase)
	mux.HandleFunc("DELETE "+dbBase+"/{dbName}", s.deleteDatabase)
	// Firewall rules
	fwBase := providerBase + "/{serverName}/firewallRules"
	mux.HandleFunc("GET "+fwBase, s.listFirewallRules)
	mux.HandleFunc("PUT "+fwBase+"/{ruleName}", s.putFirewallRule)
	mux.HandleFunc("GET "+fwBase+"/{ruleName}", s.getFirewallRule)
	mux.HandleFunc("DELETE "+fwBase+"/{ruleName}", s.deleteFirewallRule)
	// Configurations: azurerm_postgresql_flexible_server_configuration llama a
	// GET list y GET individual; devolvemos lista vacía / valores predeterminados
	// sin persistir nada, para que el provider no muestre drift.
	cfgBase := providerBase + "/{serverName}/configurations"
	mux.HandleFunc("GET "+cfgBase, s.listConfigurations)
	mux.HandleFunc("PUT "+cfgBase+"/{configName}", s.putConfiguration)
	mux.HandleFunc("GET "+cfgBase+"/{configName}", s.getConfiguration)
}

// ── Flexible Server ────────────────────────────────────────────────────────

// FlexibleServer replica el shape que azurerm_postgresql_flexible_server lee.
type FlexibleServer struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        FlexibleSku       `json:"sku"`
	Properties ServerProperties  `json:"properties"`
}

// FlexibleSku incluye name y tier. Para PostgreSQL el name lleva el prefijo
// del tier (ej. "B_Standard_B1ms"); derivamos tier del prefijo si viene vacío.
type FlexibleSku struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// ServerProperties incluye los campos que azurerm lee en cada Refresh.
type ServerProperties struct {
	AdministratorLogin       string           `json:"administratorLogin"`
	Version                  string           `json:"version"`
	State                    string           `json:"state"`
	FullyQualifiedDomainName string           `json:"fullyQualifiedDomainName"`
	Storage                  StorageProfile   `json:"storage"`
	Backup                   BackupProfile    `json:"backup"`
	HighAvailability         HighAvailability `json:"highAvailability"`
	Network                  *NetworkProfile  `json:"network,omitempty"`
}

type StorageProfile struct {
	StorageSizeGB int `json:"storageSizeGB"`
}

type BackupProfile struct {
	BackupRetentionDays int    `json:"backupRetentionDays"`
	GeoRedundantBackup  string `json:"geoRedundantBackup"`
}

type HighAvailability struct {
	Mode string `json:"mode"`
}

// NetworkProfile solo se incluye cuando el cliente lo envía; lo preservamos
// sin leerlo para que azurerm_postgresql_flexible_server no muestre drift si
// delegatedSubnetResourceId / privateDnsZoneArmResourceId fueron especificados.
type NetworkProfile struct {
	DelegatedSubnetResourceID  string `json:"delegatedSubnetResourceId,omitempty"`
	PrivateDNSZoneArmResourceID string `json:"privateDnsZoneArmResourceId,omitempty"`
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
		Network                    *NetworkProfile  `json:"network,omitempty"`
	} `json:"properties"`
}

func serverKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func serverID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.DBforPostgreSQL/flexibleServers/%s", sub, rg, name)
}

// skuTier deriva el tier del prefijo del nombre de SKU para PostgreSQL.
func skuTier(name string) string {
	switch {
	case strings.HasPrefix(name, "B_"):
		return "Burstable"
	case strings.HasPrefix(name, "GP_"):
		return "GeneralPurpose"
	case strings.HasPrefix(name, "MO_"):
		return "MemoryOptimized"
	default:
		return "GeneralPurpose"
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

	// administratorLogin es inmutable: se preserva en updates.
	adminLogin := req.Properties.AdministratorLogin
	if found && adminLogin == "" {
		adminLogin = existing.Properties.AdministratorLogin
	}

	version := req.Properties.Version
	if version == "" {
		version = "16"
	}

	var sku FlexibleSku
	if req.Sku != nil {
		sku = *req.Sku
	} else {
		sku = FlexibleSku{Name: "B_Standard_B1ms"}
	}
	if sku.Tier == "" {
		sku.Tier = skuTier(sku.Name)
	}

	storage := StorageProfile{StorageSizeGB: 32}
	if req.Properties.Storage != nil && req.Properties.Storage.StorageSizeGB > 0 {
		storage.StorageSizeGB = req.Properties.Storage.StorageSizeGB
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
			Storage:                  storage,
			Backup:                   backup,
			HighAvailability:         ha,
			Network:                  req.Properties.Network,
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
			fmt.Sprintf("PostgreSQL flexible server '%s' no encontrado", name))
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
	key := serverKey(sub, rg, name)
	_ = s.db.Delete(serversBucket, key)
	w.WriteHeader(http.StatusNoContent)
}

// ── Databases ──────────────────────────────────────────────────────────────

// PgDatabase replica azurerm_postgresql_flexible_server_database.
type PgDatabase struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Properties PgDatabaseProperties `json:"properties"`
}

type PgDatabaseProperties struct {
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
}

func dbKey(sub, rg, srvName, dbName string) string {
	return sub + "/" + rg + "/" + srvName + "/" + dbName
}
func dbID(sub, rg, srvName, dbName string) string {
	return serverID(sub, rg, srvName) + "/databases/" + dbName
}

func (s *Service) putDatabase(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srv := r.PathValue("serverName")
	name := r.PathValue("dbName")

	var req struct {
		Properties PgDatabaseProperties `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Properties.Charset == "" {
		req.Properties.Charset = "UTF8"
	}
	if req.Properties.Collation == "" {
		req.Properties.Collation = "en_US.utf8"
	}

	key := dbKey(sub, rg, srv, name)
	var existing PgDatabase
	found, _ := s.db.Get(dbsBucket, key, &existing)

	db := PgDatabase{
		ID:         dbID(sub, rg, srv, name),
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
	srv := r.PathValue("serverName")
	name := r.PathValue("dbName")
	var db PgDatabase
	found, err := s.db.Get(dbsBucket, dbKey(sub, rg, srv, name), &db)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("database '%s' no encontrado", name))
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
	srv := r.PathValue("serverName")
	items := make([]PgDatabase, 0)
	_ = s.db.List(dbsBucket, sub+"/"+rg+"/"+srv+"/", func(_ string, raw []byte) error {
		var db PgDatabase
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
	srv := r.PathValue("serverName")
	name := r.PathValue("dbName")
	_ = s.db.Delete(dbsBucket, dbKey(sub, rg, srv, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Firewall Rules ─────────────────────────────────────────────────────────

// PgFirewallRule replica azurerm_postgresql_flexible_server_firewall_rule.
type PgFirewallRule struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Properties PgFirewallRuleProperties `json:"properties"`
}

type PgFirewallRuleProperties struct {
	StartIPAddress string `json:"startIpAddress"`
	EndIPAddress   string `json:"endIpAddress"`
}

func fwKey(sub, rg, srvName, ruleName string) string {
	return sub + "/" + rg + "/" + srvName + "/" + ruleName
}
func fwID(sub, rg, srvName, ruleName string) string {
	return serverID(sub, rg, srvName) + "/firewallRules/" + ruleName
}

func (s *Service) putFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srv := r.PathValue("serverName")
	name := r.PathValue("ruleName")

	var req struct {
		Properties PgFirewallRuleProperties `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	key := fwKey(sub, rg, srv, name)
	var existing PgFirewallRule
	found, _ := s.db.Get(fwBucket, key, &existing)

	rule := PgFirewallRule{
		ID:         fwID(sub, rg, srv, name),
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
	srv := r.PathValue("serverName")
	name := r.PathValue("ruleName")
	var rule PgFirewallRule
	found, err := s.db.Get(fwBucket, fwKey(sub, rg, srv, name), &rule)
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
	srv := r.PathValue("serverName")
	items := make([]PgFirewallRule, 0)
	_ = s.db.List(fwBucket, sub+"/"+rg+"/"+srv+"/", func(_ string, raw []byte) error {
		var rule PgFirewallRule
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
	srv := r.PathValue("serverName")
	name := r.PathValue("ruleName")
	_ = s.db.Delete(fwBucket, fwKey(sub, rg, srv, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Configurations (stub) ─────────────────────────────────────────────────

// configResponse devuelve la forma mínima que azurerm espera al leer una
// configuración individual. No persistimos nada — los valores de
// configuración son efímeros, lo mismo que hacemos para backupPolicies en
// recoveryservices.
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
		"type": "Microsoft.DBforPostgreSQL/flexibleServers/configurations",
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
	// Aceptar sin persistir — el valor se pierde al reiniciar, mismo
	// que backupConfig en recoveryservices.
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
		"type": "Microsoft.DBforPostgreSQL/flexibleServers/configurations",
		"properties": map[string]any{
			"value":        req.Properties.Value,
			"defaultValue": "",
			"dataType":     "String",
			"source":       "user-override",
		},
	})
}
