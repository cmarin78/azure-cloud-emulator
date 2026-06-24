package containerregistry

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

// TestRegistryLifecycle cubre el ARM CRUD síncrono completo de
// Microsoft.ContainerRegistry/registries: PUT (create), PUT (update
// idempotente, preservando creationDate entre actualizaciones), GET, LIST
// y DELETE (idempotente, 204 si ya no existe).
func TestRegistryLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries/myregistry"

	var created Registry
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "test"},
		"sku":      map[string]any{"name": "Basic"},
		"properties": map[string]any{
			"adminUserEnabled": true,
		},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put registry: status=%d registry=%+v", status, created)
	}
	if created.Properties.LoginServer != "myregistry.azurecr.io" {
		t.Fatalf("unexpected loginServer: %q", created.Properties.LoginServer)
	}
	if !created.Properties.AdminUserEnabled {
		t.Fatalf("expected adminUserEnabled to be true, got %+v", created.Properties)
	}
	if created.Sku.Name != "Basic" {
		t.Fatalf("unexpected sku: %+v", created.Sku)
	}

	var updated Registry
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "prod"},
		"sku":      map[string]any{"name": "Basic"},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("update registry: want 200, got %d", status)
	}
	if updated.Properties.CreationDate != created.Properties.CreationDate {
		t.Fatalf("expected creationDate to survive update, got %q vs %q",
			created.Properties.CreationDate, updated.Properties.CreationDate)
	}
	if updated.Tags["env"] != "prod" {
		t.Fatalf("expected tags to update, got %+v", updated.Tags)
	}

	var got Registry
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myregistry" {
		t.Fatalf("get registry: status=%d registry=%+v", status, got)
	}

	var list struct {
		Value []Registry `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list registries: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete registry: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete registry: want 204, got %d", status)
	}
}

func TestRegistryRequiresSku(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries/myregistry")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{
		"location": "eastus",
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing sku: want 400, got %d", status)
	}
}

func TestRegistryNotFound(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries/missing"
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404, got %d", status)
	}
}
