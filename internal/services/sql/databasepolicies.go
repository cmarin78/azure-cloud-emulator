package sql

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// registerDatabasePolicies monta tres sub-recursos singleton de
// databases que azurerm_mssql_database consulta incondicionalmente en su
// Read (longtermretentionpolicies, backupshorttermretentionpolicies,
// databasesecurityalertpolicies -- mapeados a los atributos
// long_term_retention_policy/short_term_retention_policy/
// threat_detection_policy del recurso). No hay motor de retención/alertas
// real en este emulador -- siempre se responde el default "todo
// deshabilitado", mismo patrón "shape-compatible, no behavior-complete"
// que connectionPolicies/restorableDroppedDatabases.
func (s *Service) registerDatabasePolicies(mux *http.ServeMux) {
	dbBase := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers/{serverName}/databases/{databaseName}"

	mux.HandleFunc("GET "+dbBase+"/backupLongTermRetentionPolicies/default", s.getLongTermRetentionPolicy)
	mux.HandleFunc("GET "+dbBase+"/backupShortTermRetentionPolicies/default", s.getShortTermRetentionPolicy)
	mux.HandleFunc("GET "+dbBase+"/securityAlertPolicies/Default", s.getSecurityAlertPolicy)
	mux.HandleFunc("GET "+dbBase+"/transparentDataEncryption/current", s.getTransparentDataEncryption)
}

func (s *Service) getTransparentDataEncryption(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")
	id := "/subscriptions/" + r.PathValue("subscriptionId") + "/resourceGroups/" + r.PathValue("resourceGroupName") +
		"/providers/Microsoft.Sql/servers/" + srvName + "/databases/" + dbName + "/transparentDataEncryption/current"

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"name": "current",
		"type": "Microsoft.Sql/servers/databases/transparentDataEncryption",
		"properties": map[string]any{
			"state": "Enabled",
		},
	})
}

func (s *Service) getLongTermRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")
	id := "/subscriptions/" + r.PathValue("subscriptionId") + "/resourceGroups/" + r.PathValue("resourceGroupName") +
		"/providers/Microsoft.Sql/servers/" + srvName + "/databases/" + dbName + "/backupLongTermRetentionPolicies/default"

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"name": "default",
		"type": "Microsoft.Sql/servers/databases/backupLongTermRetentionPolicies",
		"properties": map[string]any{
			"weeklyRetention":  "PT0S",
			"monthlyRetention": "PT0S",
			"yearlyRetention":  "PT0S",
			"weekOfYear":       0,
		},
	})
}

func (s *Service) getShortTermRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")
	id := "/subscriptions/" + r.PathValue("subscriptionId") + "/resourceGroups/" + r.PathValue("resourceGroupName") +
		"/providers/Microsoft.Sql/servers/" + srvName + "/databases/" + dbName + "/backupShortTermRetentionPolicies/default"

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"name": "default",
		"type": "Microsoft.Sql/servers/databases/backupShortTermRetentionPolicies",
		"properties": map[string]any{
			"retentionDays": 7,
		},
	})
}

func (s *Service) getSecurityAlertPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	srvName := r.PathValue("serverName")
	dbName := r.PathValue("databaseName")
	id := "/subscriptions/" + r.PathValue("subscriptionId") + "/resourceGroups/" + r.PathValue("resourceGroupName") +
		"/providers/Microsoft.Sql/servers/" + srvName + "/databases/" + dbName + "/securityAlertPolicies/Default"

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"name": "Default",
		"type": "Microsoft.Sql/servers/databases/securityAlertPolicies",
		"properties": map[string]any{
			"state":                   "Disabled",
			"disabledAlerts":          []string{},
			"emailAddresses":          []string{},
			"emailAccountAdmins":      false,
			"retentionDays":           0,
			"storageEndpoint":         "",
			"storageAccountAccessKey": "",
		},
	})
}
