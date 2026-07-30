package signalr

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func svcURL(base, sub, rg, name string) string {
	return base + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.SignalRService/signalR/" + name
}

// TestSignalRCRUD prueba PUT → GET → LIST → DELETE.
func TestSignalRCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := svcURL(srv.URL, "sub1", "rg1", "testsignalr")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.SignalRService/signalR"

	// PUT (create) → 201
	var created SignalRService
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Free_F1", "capacity": 1},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put create: status=%d", status)
	}
	if created.Name != "testsignalr" {
		t.Fatalf("name=%q", created.Name)
	}
	if !strings.HasSuffix(created.Properties.HostName, ".service.signalr.net") {
		t.Fatalf("hostname=%q", created.Properties.HostName)
	}
	if created.Properties.PublicPort != 443 {
		t.Fatalf("publicPort=%d", created.Properties.PublicPort)
	}
	if created.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("provisioningState=%q", created.Properties.ProvisioningState)
	}
	if created.Sku.Tier != "Free" {
		t.Fatalf("sku.tier=%q (want Free)", created.Sku.Tier)
	}

	// PUT idempotente → 200
	var updated SignalRService
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard_S1", "capacity": 1},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put update: status=%d", status)
	}
	if updated.Sku.Tier != "Standard" {
		t.Fatalf("updated sku.tier=%q", updated.Sku.Tier)
	}

	// GET → 200
	var got SignalRService
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "testsignalr" {
		t.Fatalf("get: status=%d name=%q", status, got.Name)
	}

	// LIST → 1 item
	var list struct{ Value []SignalRService }
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

// TestSignalRDefaults verifica los valores predeterminados.
func TestSignalRDefaults(t *testing.T) {
	srv := newTestServer(t)
	base := svcURL(srv.URL, "sub1", "rg1", "defaultsignalr")

	var svc SignalRService
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
	}, &svc)

	if svc.Sku.Name != "Free_F1" {
		t.Fatalf("default sku: %q", svc.Sku.Name)
	}
	if svc.Sku.Capacity != 1 {
		t.Fatalf("default capacity: %d", svc.Sku.Capacity)
	}
	if svc.Properties.ServiceMode != "Default" {
		t.Fatalf("default serviceMode: %q", svc.Properties.ServiceMode)
	}
	if svc.Properties.Version != "1.0" {
		t.Fatalf("version: %q", svc.Properties.Version)
	}
}

// TestSignalRListKeys verifica que listKeys devuelve connection strings.
func TestSignalRListKeys(w *testing.T) {
	srv := newTestServer(w)
	base := svcURL(srv.URL, "sub1", "rg1", "keysignalr")

	testutil.DoJSON(w, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
	}, nil)

	var keys map[string]any
	status := testutil.DoJSON(w, "POST", testutil.WithAPIVersion(base+"/listKeys"), map[string]any{}, &keys)
	if status != http.StatusOK {
		w.Fatalf("listKeys: status=%d", status)
	}
	primary, _ := keys["primaryConnectionString"].(string)
	if !strings.Contains(primary, "keysignalr") {
		w.Fatalf("primaryConnectionString=%q, expected to contain 'keysignalr'", primary)
	}
	if !strings.Contains(primary, "Endpoint=") {
		w.Fatalf("primaryConnectionString=%q, missing Endpoint=", primary)
	}
}
