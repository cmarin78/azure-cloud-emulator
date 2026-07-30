package mysql

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

func srvURL(base, sub, rg, name string) string {
	return base + "/subscriptions/" + sub +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.DBforMySQL/flexibleServers/" + name
}

// TestMySQLServerCRUD prueba PUT → GET → LIST → DELETE.
func TestMySQLServerCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "mysqltest")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DBforMySQL/flexibleServers"

	// PUT (create) → 201
	var created FlexibleServer
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard_B1ms", "tier": "Burstable"},
		"properties": map[string]any{
			"administratorLogin":         "mysqladmin",
			"administratorLoginPassword": "P@ssw0rd!",
			"version":                    "8.0.21",
		},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put create: status=%d", status)
	}
	if created.Name != "mysqltest" {
		t.Fatalf("name=%q", created.Name)
	}
	if created.Properties.State != "Ready" {
		t.Fatalf("state=%q", created.Properties.State)
	}
	if created.Properties.FullyQualifiedDomainName != "mysqltest.mysql.database.azure.com" {
		t.Fatalf("fqdn=%q", created.Properties.FullyQualifiedDomainName)
	}
	if created.Sku.Tier != "Burstable" {
		t.Fatalf("sku.tier=%q", created.Sku.Tier)
	}
	if created.Properties.ReplicationRole != "None" {
		t.Fatalf("replicationRole=%q", created.Properties.ReplicationRole)
	}

	// PUT idempotente → 200
	var updated FlexibleServer
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard_D2ds_v4", "tier": "GeneralPurpose"},
		"properties": map[string]any{
			"administratorLogin": "mysqladmin",
			"version":            "8.0.21",
		},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put update: status=%d", status)
	}
	if updated.Sku.Name != "Standard_D2ds_v4" {
		t.Fatalf("updated sku.name=%q", updated.Sku.Name)
	}

	// GET → 200
	var got FlexibleServer
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "mysqltest" {
		t.Fatalf("get: status=%d name=%q", status, got.Name)
	}

	// LIST → 1 item
	var list struct{ Value []FlexibleServer }
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list: status=%d items=%d", status, len(list.Value))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status=%d", status)
	}

	// GET after delete → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: want 404, got %d", status)
	}
}

// TestMySQLDefaults verifica los valores predeterminados.
func TestMySQLDefaults(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "mysqldefault")

	var s FlexibleServer
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"administratorLogin": "admin"},
	}, &s)

	if s.Properties.Version != "8.0.21" {
		t.Fatalf("default version: %q", s.Properties.Version)
	}
	if s.Properties.Backup.BackupRetentionDays != 7 {
		t.Fatalf("default backup days: %d", s.Properties.Backup.BackupRetentionDays)
	}
	if s.Properties.Storage.AutoGrow != "Enabled" {
		t.Fatalf("default auto grow: %q", s.Properties.Storage.AutoGrow)
	}
	if s.Sku.Name != "Standard_B1ms" {
		t.Fatalf("default sku: %q", s.Sku.Name)
	}
	if s.Properties.ReplicaCapacity != 5 {
		t.Fatalf("replicaCapacity: %d", s.Properties.ReplicaCapacity)
	}
}

// TestMySQLSubResources prueba databases + firewallRules + configurations.
func TestMySQLSubResources(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "mysqlsub")

	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"administratorLogin": "admin"},
	}, nil)

	// PUT database → 201
	dbURL := testutil.WithAPIVersion(base + "/databases/appdb")
	var db MySQLDatabase
	status := testutil.DoJSON(t, "PUT", dbURL, map[string]any{
		"properties": map[string]any{"charset": "utf8mb4", "collation": "utf8mb4_unicode_ci"},
	}, &db)
	if status != http.StatusCreated {
		t.Fatalf("put db: status=%d", status)
	}
	if db.Properties.Charset != "utf8mb4" {
		t.Fatalf("db charset: %q", db.Properties.Charset)
	}

	// LIST databases → 1
	var dbList struct{ Value []MySQLDatabase }
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/databases"), nil, &dbList)
	if len(dbList.Value) != 1 {
		t.Fatalf("list db: %d", len(dbList.Value))
	}

	// DELETE database → 204
	status = testutil.DoJSON(t, "DELETE", dbURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete db: status=%d", status)
	}

	// PUT firewall rule → 201
	fwURL := testutil.WithAPIVersion(base + "/firewallRules/AllowAll")
	var fw MySQLFirewallRule
	status = testutil.DoJSON(t, "PUT", fwURL, map[string]any{
		"properties": map[string]any{"startIpAddress": "0.0.0.0", "endIpAddress": "255.255.255.255"},
	}, &fw)
	if status != http.StatusCreated {
		t.Fatalf("put fw: status=%d", status)
	}

	// GET firewall rule → 200
	status = testutil.DoJSON(t, "GET", fwURL, nil, &fw)
	if status != http.StatusOK {
		t.Fatalf("get fw: status=%d", status)
	}

	// DELETE firewall rule → 204
	status = testutil.DoJSON(t, "DELETE", fwURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete fw: status=%d", status)
	}

	// GET configurations (list) → empty
	var cfgList struct{ Value []any }
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/configurations"), nil, &cfgList)
	if len(cfgList.Value) != 0 {
		t.Fatalf("configurations not empty: %d", len(cfgList.Value))
	}

	// GET configuration (individual) → 200
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/configurations/max_connections"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get config: status=%d", status)
	}
}
