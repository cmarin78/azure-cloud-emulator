package sql

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	mux := http.NewServeMux()
	New(db).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestServerLifecycle cubre el ARM CRUD síncrono completo de
// Microsoft.Sql/servers: PUT (create), PUT (update idempotente,
// preservando administratorLogin/version entre actualizaciones), GET,
// LIST y DELETE (idempotente, 204 si ya no existe).
func TestServerLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/myserver"

	var created Server
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "test"},
		"properties": map[string]any{
			"administratorLogin":         "sqladmin",
			"administratorLoginPassword": "doesnotmatter",
		},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put server: status=%d server=%+v", status, created)
	}
	if created.Properties.AdministratorLogin != "sqladmin" {
		t.Fatalf("expected administratorLogin to be set, got %+v", created.Properties)
	}
	if created.Properties.FullyQualifiedDomainName != "myserver.database.windows.net" {
		t.Fatalf("unexpected fullyQualifiedDomainName: %q", created.Properties.FullyQualifiedDomainName)
	}
	if created.Properties.Version != "12.0" || created.Properties.MinimalTlsVersion != "1.2" {
		t.Fatalf("unexpected defaults: %+v", created.Properties)
	}

	// Un segundo PUT (update) debe preservar administratorLogin/version,
	// igual que managedidentity preserva tenantId/principalId/clientId.
	var updated Server
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "prod"},
		"properties": map[string]any{
			"administratorLogin": "otheradmin",
		},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("update server: want 200, got %d", status)
	}
	if updated.Properties.AdministratorLogin != "sqladmin" {
		t.Fatalf("expected administratorLogin to survive update, got %q", updated.Properties.AdministratorLogin)
	}
	if updated.Tags["env"] != "prod" {
		t.Fatalf("expected tags to update, got %+v", updated.Tags)
	}

	var got Server
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myserver" {
		t.Fatalf("get server: status=%d server=%+v", status, got)
	}

	var list struct {
		Value []Server `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list servers: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete server: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete server: want 204, got %d", status)
	}
}

func TestServerRequiresAdministratorLogin(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/myserver")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{
		"location": "eastus",
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing administratorLogin: want 400, got %d", status)
	}
}

// TestDatabaseLifecycle cubre el ARM CRUD síncrono de
// Microsoft.Sql/servers/databases, sub-recurso anidado de un solo nivel.
func TestDatabaseLifecycle(t *testing.T) {
	srv := newTestServer(t)
	serverBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/myserver"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(serverBase), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"administratorLogin": "sqladmin",
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put server: status=%d", status)
	}

	dbBase := serverBase + "/databases/mydb"
	var db Database
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(dbBase), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Basic"},
	}, &db)
	if status != http.StatusCreated {
		t.Fatalf("put database: status=%d db=%+v", status, db)
	}
	if db.Properties.Status != "Online" || db.Properties.Collation == "" {
		t.Fatalf("unexpected database properties: %+v", db.Properties)
	}

	var list struct {
		Value []Database `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(serverBase+"/databases"), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list databases: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(dbBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete database: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(dbBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete database: want 204, got %d", status)
	}
}

func TestDatabaseRequiresExistingServer(t *testing.T) {
	srv := newTestServer(t)
	dbBase := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/missing/databases/mydb")
	status := testutil.DoJSON(t, "PUT", dbBase, map[string]any{"location": "eastus"}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for missing parent server, got %d", status)
	}
}

// TestFirewallRuleLifecycle cubre el ARM CRUD síncrono de
// Microsoft.Sql/servers/firewallRules.
func TestFirewallRuleLifecycle(t *testing.T) {
	srv := newTestServer(t)
	serverBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/myserver"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(serverBase), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"administratorLogin": "sqladmin",
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put server: status=%d", status)
	}

	ruleBase := serverBase + "/firewallRules/AllowAll"
	var rule FirewallRule
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(ruleBase), map[string]any{
		"properties": map[string]any{
			"startIpAddress": "0.0.0.0",
			"endIpAddress":   "255.255.255.255",
		},
	}, &rule)
	if status != http.StatusCreated {
		t.Fatalf("put firewall rule: status=%d rule=%+v", status, rule)
	}
	if rule.Properties.StartIPAddress != "0.0.0.0" {
		t.Fatalf("unexpected firewall rule properties: %+v", rule.Properties)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(ruleBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete firewall rule: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(ruleBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete firewall rule: want 204, got %d", status)
	}
}

func TestFirewallRuleRequiresIPRange(t *testing.T) {
	srv := newTestServer(t)
	serverBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Sql/servers/myserver"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(serverBase), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"administratorLogin": "sqladmin",
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put server: status=%d", status)
	}

	ruleBase := testutil.WithAPIVersion(serverBase + "/firewallRules/bad")
	status = testutil.DoJSON(t, "PUT", ruleBase, map[string]any{"properties": map[string]any{}}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing IP range: want 400, got %d", status)
	}
}
