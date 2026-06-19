package storageaccounts

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	ops := server.NewOperations()
	mux := http.NewServeMux()
	New(db, ops).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestStorageAccountLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1"

	var acct StorageAccount
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), storageAccountRequest{
		Location: "eastus", Sku: Sku{Name: "Standard_LRS"},
	}, &acct)
	if status != http.StatusAccepted {
		t.Fatalf("put: status=%d", status)
	}
	if acct.Properties.ProvisioningState != "Succeeded" || acct.Properties.PrimaryEndpoints.Blob == "" {
		t.Fatalf("put response missing fields: %+v", acct)
	}
	if acct.Kind != "StorageV2" {
		t.Fatalf("expected default kind StorageV2, got %q", acct.Kind)
	}

	var got StorageAccount
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "acct1" {
		t.Fatalf("get: status=%d acct=%+v", status, got)
	}

	var list struct {
		Value []StorageAccount `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete: status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}

	// Idempotent delete (already gone) returns 204.
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete: want 204, got %d", status)
	}
}

func TestStorageAccountRequiresLocationAndSku(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1")

	status := testutil.DoJSON(t, "PUT", base, storageAccountRequest{Sku: Sku{Name: "Standard_LRS"}}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}

	status = testutil.DoJSON(t, "PUT", base, storageAccountRequest{Location: "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing sku: want 400, got %d", status)
	}
}

func TestGetMissingStorageAccountReturns404(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/missing")
	status := testutil.DoJSON(t, "GET", base, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404, got %d", status)
	}
}
