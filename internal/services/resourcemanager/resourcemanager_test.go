package resourcemanager

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
	server.RegisterOperations(mux, ops)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestSubscriptionAutoVivifies confirma que cualquier subscriptionId es
// aceptado sin necesidad de haberlo creado antes.
func TestSubscriptionAutoVivifies(t *testing.T) {
	srv := newTestServer(t)
	var sub Subscription
	status := testutil.DoJSON(t, "GET", srv.URL+"/subscriptions/any-guid", nil, &sub)
	if status != 200 || sub.State != "Enabled" || sub.SubscriptionID != "any-guid" {
		t.Fatalf("getSubscription: status=%d sub=%+v", status, sub)
	}
}

// TestResourceGroupLifecycle cubre put -> get -> list -> delete (async),
// confirmando el polling de la operación devuelve Succeeded.
func TestResourceGroupLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/my-rg"

	var rg ResourceGroup
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]string{"location": "eastus"}, &rg)
	if status != http.StatusCreated || rg.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put: status=%d rg=%+v", status, rg)
	}

	var got ResourceGroup
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "my-rg" {
		t.Fatalf("get: status=%d rg=%+v", status, got)
	}

	var list struct {
		Value []ResourceGroup `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/resourceGroups"), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete: status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}

	// Deleting again (already gone) must be idempotent (204), matching ARM.
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete: want 204, got %d", status)
	}
}

func TestMissingAPIVersionRejected(t *testing.T) {
	srv := newTestServer(t)
	status := testutil.DoJSON(t, "GET", srv.URL+"/subscriptions/sub1/resourceGroups", nil, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 without api-version, got %d", status)
	}
}

func TestMissingLocationRejected(t *testing.T) {
	srv := newTestServer(t)
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/resourceGroups/rg"), map[string]string{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 without location, got %d", status)
	}
}

// TestProviders confirms a known namespace reports Registered and an
// unknown one reports NotRegistered (instead of 404).
func TestProviders(t *testing.T) {
	srv := newTestServer(t)

	var p Provider
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/providers/Microsoft.Storage"), nil, &p)
	if status != 200 || p.RegistrationState != "Registered" {
		t.Fatalf("known provider: status=%d p=%+v", status, p)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/providers/Microsoft.Unknown"), nil, &p)
	if status != 200 || p.RegistrationState != "NotRegistered" {
		t.Fatalf("unknown provider: status=%d p=%+v", status, p)
	}

	var list struct {
		Value []Provider `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/providers"), nil, &list)
	if status != 200 || len(list.Value) == 0 {
		t.Fatalf("list providers: status=%d value=%+v", status, list.Value)
	}
}

// TestListResourcesInGroupAlwaysEmpty documents the deliberate stub: this
// emulator has no cross-service resource index, so it always returns an
// empty list (good enough for `terraform destroy` ordering checks).
func TestListResourcesInGroupAlwaysEmpty(t *testing.T) {
	srv := newTestServer(t)
	var list struct {
		Value []any `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/resourceGroups/rg/resources"), nil, &list)
	if status != 200 || len(list.Value) != 0 {
		t.Fatalf("status=%d value=%+v", status, list.Value)
	}
}

// TestOperationsPolling confirms the shared LRO endpoint reports Succeeded
// for an operation created via WriteAccepted (resource group delete).
func TestOperationsPolling(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg-for-ops"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]string{"location": "eastus"}, nil)

	resp, err := http.DefaultClient.Do(mustReq(t, "DELETE", testutil.WithAPIVersion(base)))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	opURL := resp.Header.Get("Azure-AsyncOperation")
	resp.Body.Close()
	if opURL == "" {
		t.Fatalf("expected Azure-AsyncOperation header")
	}

	var op server.OperationStatus
	status := testutil.DoJSON(t, "GET", opURL, nil, &op)
	if status != 200 || op.Status != "Succeeded" {
		t.Fatalf("operationsStatus: status=%d op=%+v", status, op)
	}
}

func mustReq(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
