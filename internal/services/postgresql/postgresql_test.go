package postgresql

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
		"/providers/Microsoft.DBforPostgreSQL/flexibleServers/" + name
}

// TestPostgreSQLServerCRUD prueba PUT → GET → LIST → DELETE del servidor.
func TestPostgreSQLServerCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "pgtest")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DBforPostgreSQL/flexibleServers"

	// PUT (create) → 201
	var created FlexibleServer
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "B_Standard_B1ms"},
		"properties": map[string]any{
			"administratorLogin":         "pgadmin",
			"administratorLoginPassword": "P@ssw0rd!",
			"version":                    "16",
		},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put create: status=%d", status)
	}
	if created.Name != "pgtest" {
		t.Fatalf("name=%q", created.Name)
	}
	if created.Properties.State != "Ready" {
		t.Fatalf("state=%q", created.Properties.State)
	}
	if created.Properties.FullyQualifiedDomainName != "pgtest.postgres.database.azure.com" {
		t.Fatalf("fqdn=%q", created.Properties.FullyQualifiedDomainName)
	}
	if created.Sku.Tier != "Burstable" {
		t.Fatalf("sku.tier=%q (want Burstable)", created.Sku.Tier)
	}

	// PUT idempotente → 200
	var updated FlexibleServer
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "GP_Standard_D2s_v3"},
		"properties": map[string]any{
			"administratorLogin": "pgadmin",
			"version":            "16",
		},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put update: status=%d", status)
	}
	if updated.Sku.Name != "GP_Standard_D2s_v3" {
		t.Fatalf("updated sku.name=%q", updated.Sku.Name)
	}
	if updated.Sku.Tier != "GeneralPurpose" {
		t.Fatalf("updated sku.tier=%q", updated.Sku.Tier)
	}

	// GET → 200
	var got FlexibleServer
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "pgtest" {
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

// TestPostgreSQLDefaults verifica los valores predeterminados.
func TestPostgreSQLDefaults(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "pgdefault")

	var s FlexibleServer
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"administratorLogin": "admin"},
	}, &s)

	if s.Properties.Version != "16" {
		t.Fatalf("default version: %q", s.Properties.Version)
	}
	if s.Properties.Backup.BackupRetentionDays != 7 {
		t.Fatalf("default backup days: %d", s.Properties.Backup.BackupRetentionDays)
	}
	if s.Properties.Backup.GeoRedundantBackup != "Disabled" {
		t.Fatalf("default geo backup: %q", s.Properties.Backup.GeoRedundantBackup)
	}
	if s.Properties.HighAvailability.Mode != "Disabled" {
		t.Fatalf("default HA mode: %q", s.Properties.HighAvailability.Mode)
	}
	if s.Properties.Storage.StorageSizeGB != 32 {
		t.Fatalf("default storage: %d", s.Properties.Storage.StorageSizeGB)
	}
	if s.Sku.Name != "B_Standard_B1ms" {
		t.Fatalf("default sku: %q", s.Sku.Name)
	}
}

// TestPostgreSQLSubResources prueba databases + firewallRules + configurations.
func TestPostgreSQLSubResources(t *testing.T) {
	srv := newTestServer(t)
	base := srvURL(srv.URL, "sub1", "rg1", "pgsubtest")

	// Crear servidor
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"administratorLogin": "admin"},
	}, nil)

	// PUT database → 201
	dbURL := testutil.WithAPIVersion(base + "/databases/mydb")
	var db PgDatabase
	status := testutil.DoJSON(t, "PUT", dbURL, map[string]any{
		"properties": map[string]any{"charset": "UTF8", "collation": "en_US.utf8"},
	}, &db)
	if status != http.StatusCreated {
		t.Fatalf("put db: status=%d", status)
	}
	if db.Properties.Charset != "UTF8" {
		t.Fatalf("db charset: %q", db.Properties.Charset)
	}

	// GET database → 200
	status = testutil.DoJSON(t, "GET", dbURL, nil, &db)
	if status != http.StatusOK {
		t.Fatalf("get db: status=%d", status)
	}

	// LIST databases → 1
	var dbList struct{ Value []PgDatabase }
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/databases"), nil, &dbList)
	if len(dbList.Value) != 1 {
		t.Fatalf("list db: %d items", len(dbList.Value))
	}

	// DELETE database → 204
	status = testutil.DoJSON(t, "DELETE", dbURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete db: status=%d", status)
	}

	// PUT firewall rule → 201
	fwURL := testutil.WithAPIVersion(base + "/firewallRules/AllowAll")
	var fw PgFirewallRule
	status = testutil.DoJSON(t, "PUT", fwURL, map[string]any{
		"properties": map[string]any{"startIpAddress": "0.0.0.0", "endIpAddress": "255.255.255.255"},
	}, &fw)
	if status != http.StatusCreated {
		t.Fatalf("put fw: status=%d", status)
	}
	if fw.Properties.StartIPAddress != "0.0.0.0" {
		t.Fatalf("fw start ip: %q", fw.Properties.StartIPAddress)
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
		t.Fatalf("configurations list not empty: %d", len(cfgList.Value))
	}

	// GET configuration (individual) → 200
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/configurations/max_connections"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get config: status=%d", status)
	}
}
