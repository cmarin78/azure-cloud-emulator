package recoveryservices

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

func vaultURL(srvURL, sub, rg, name string) string {
	return srvURL + "/subscriptions/" + sub +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.RecoveryServices/vaults/" + name
}

// TestVaultCRUD prueba PUT → GET → LIST → DELETE.
func TestVaultCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := vaultURL(srv.URL, "sub1", "rg1", "myvault")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.RecoveryServices/vaults"

	// PUT (create) → 201
	var created Vault
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard"},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put (create): status=%d", status)
	}
	if created.Name != "myvault" {
		t.Fatalf("put: name=%q", created.Name)
	}
	if created.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put: provisioningState=%q", created.Properties.ProvisioningState)
	}
	if created.Sku.Name != "Standard" {
		t.Fatalf("put: sku.name=%q", created.Sku.Name)
	}
	if created.ID == "" {
		t.Fatal("put: ID empty")
	}

	// PUT idempotente → 200
	var updated Vault
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "RS0"},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put (update): status=%d", status)
	}
	if updated.Sku.Name != "RS0" {
		t.Fatalf("put (update): sku.name=%q", updated.Sku.Name)
	}

	// GET → 200
	var got Vault
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myvault" {
		t.Fatalf("get: status=%d name=%q", status, got.Name)
	}

	// LIST → 1 item
	var list struct {
		Value []Vault `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list: status=%d items=%d", status, len(list.Value))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status=%d", status)
	}

	// GET después de borrar → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: want 404, got %d", status)
	}

	// DELETE idempotente → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete idempotente: status=%d", status)
	}
}

// TestVaultDefaults verifica sku=Standard y publicNetworkAccess=Enabled.
func TestVaultDefaults(t *testing.T) {
	srv := newTestServer(t)
	base := vaultURL(srv.URL, "sub1", "rg1", "defaultvault")

	var v Vault
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
	}, &v)
	if status != http.StatusCreated {
		t.Fatalf("put defaults: status=%d", status)
	}
	if v.Sku.Name != "Standard" {
		t.Fatalf("default sku: want Standard, got %q", v.Sku.Name)
	}
	if v.Properties.PublicNetworkAccess != "Enabled" {
		t.Fatalf("default publicNetworkAccess: want Enabled, got %q", v.Properties.PublicNetworkAccess)
	}
}

// TestVaultSubResources verifica los endpoints read-only que azurerm lee en Refresh.
func TestVaultSubResources(t *testing.T) {
	srv := newTestServer(t)
	base := vaultURL(srv.URL, "sub1", "rg1", "subvault")

	// Crear vault
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"location": "eastus"}, nil)

	// GET backupConfig/vaultconfig → 200
	var bc map[string]any
	status := testutil.DoJSON(t, "GET",
		testutil.WithAPIVersion(base+"/backupConfig/vaultconfig"), nil, &bc)
	if status != http.StatusOK {
		t.Fatalf("backupConfig GET: status=%d", status)
	}
	props := bc["properties"].(map[string]any)
	if props["softDeleteFeatureState"] != "Enabled" {
		t.Fatalf("backupConfig: softDeleteFeatureState=%v", props["softDeleteFeatureState"])
	}

	// PUT backupConfig/vaultconfig → 200 (acepta sin error)
	status = testutil.DoJSON(t, "PUT",
		testutil.WithAPIVersion(base+"/backupConfig/vaultconfig"),
		map[string]any{"properties": map[string]any{"softDeleteFeatureState": "Disabled"}}, &bc)
	if status != http.StatusOK {
		t.Fatalf("backupConfig PUT: status=%d", status)
	}

	// GET backupPolicies → 200 lista vacía
	var policies map[string]any
	status = testutil.DoJSON(t, "GET",
		testutil.WithAPIVersion(base+"/backupPolicies"), nil, &policies)
	if status != http.StatusOK {
		t.Fatalf("backupPolicies GET: status=%d", status)
	}
	if v, ok := policies["value"].([]any); !ok || len(v) != 0 {
		t.Fatalf("backupPolicies: expected empty list")
	}

	// GET backupStorageConfig/vaultStorageConfig → 200
	var sc map[string]any
	status = testutil.DoJSON(t, "GET",
		testutil.WithAPIVersion(base+"/backupStorageConfig/vaultStorageConfig"), nil, &sc)
	if status != http.StatusOK {
		t.Fatalf("backupStorageConfig GET: status=%d", status)
	}
	scProps := sc["properties"].(map[string]any)
	if scProps["storageType"] != "GeoRedundant" {
		t.Fatalf("backupStorageConfig: storageType=%v", scProps["storageType"])
	}
}
