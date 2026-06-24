// Package sql emula el subconjunto de Microsoft.Sql necesario para que
// az/Terraform (azurerm_mssql_server, azurerm_mssql_database,
// azurerm_sql_firewall_rule/azurerm_mssql_firewall_rule) operen extremo a
// extremo: servers (ARM CRUD síncrono -- crear un logical server en Azure
// real no requiere polling en los flujos comunes, igual que
// managedidentity.UserAssignedIdentity/keyvault.Vault), y dos sub-recursos
// anidados de un solo nivel (databases, firewallRules), también síncronos,
// mismo patrón que eventhub.EventHubResource/ConsumerGroup.
//
// No hay motor de consultas real: las databases son registros ARM con
// propiedades fake (sku, collation, maxSizeBytes) -- "shape-compatible, no
// behavior-complete" como el resto de los data planes simplificados de este
// proyecto. administratorLoginPassword se acepta en el PUT pero, igual que
// compute.OsProfile.AdminPassword, nunca se persiste ni se devuelve.
package sql

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.Sql (servers, databases, firewallRules).
type Service struct {
	db *storage.DB
}

// New crea el servicio de SQL Database.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta las rutas ARM de Microsoft.Sql.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerServers(mux)
	s.registerDatabases(mux)
	s.registerFirewallRules(mux)
	s.registerConnectionPolicies(mux)
	s.registerDatabasePolicies(mux)
}
