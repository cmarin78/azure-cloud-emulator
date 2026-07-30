package notificationhubs

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

func nsURL(base, sub, rg, ns string) string {
	return base + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.NotificationHubs/namespaces/" + ns
}

// TestNamespaceCRUD prueba PUT → GET → LIST → DELETE del namespace.
func TestNamespaceCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := nsURL(srv.URL, "sub1", "rg1", "testns")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.NotificationHubs/namespaces"

	// PUT (create) → 201
	var created Namespace
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Free"},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put create: status=%d", status)
	}
	if created.Name != "testns" {
		t.Fatalf("name=%q", created.Name)
	}
	if created.Properties.Status != "Active" {
		t.Fatalf("status=%q", created.Properties.Status)
	}
	if created.Properties.ServiceBusEndpoint == "" {
		t.Fatal("serviceBusEndpoint empty")
	}

	// PUT idempotente → 200
	var updated Namespace
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard"},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put update: status=%d", status)
	}
	if updated.Sku.Name != "Standard" {
		t.Fatalf("sku.name=%q", updated.Sku.Name)
	}

	// GET → 200
	var got Namespace
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "testns" {
		t.Fatalf("get: status=%d name=%q", status, got.Name)
	}

	// LIST → 1 item
	var list struct{ Value []Namespace }
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

// TestHubCRUD prueba PUT → GET → LIST → DELETE del notification hub.
func TestHubCRUD(t *testing.T) {
	srv := newTestServer(t)
	nsBase := nsURL(srv.URL, "sub1", "rg1", "testns")

	// Crear namespace
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsBase), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Free"},
	}, nil)

	hubBase := testutil.WithAPIVersion(nsBase + "/notificationHubs/testhub")
	listHubs := testutil.WithAPIVersion(nsBase + "/notificationHubs")

	// PUT hub → 201
	var hub Hub
	status := testutil.DoJSON(t, "PUT", hubBase, map[string]any{
		"location": "eastus",
	}, &hub)
	if status != http.StatusCreated {
		t.Fatalf("put hub: status=%d", status)
	}
	if hub.Name != "testhub" {
		t.Fatalf("hub name=%q", hub.Name)
	}
	if hub.Properties.RegistrationTTL == "" {
		t.Fatal("registrationTtl empty")
	}

	// GET hub → 200
	status = testutil.DoJSON(t, "GET", hubBase, nil, &hub)
	if status != http.StatusOK {
		t.Fatalf("get hub: status=%d", status)
	}

	// LIST hubs → 1
	var list struct{ Value []Hub }
	status = testutil.DoJSON(t, "GET", listHubs, nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list hubs: status=%d items=%d", status, len(list.Value))
	}

	// DELETE hub → 204
	status = testutil.DoJSON(t, "DELETE", hubBase, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete hub: status=%d", status)
	}
}

// TestHubAuthRules prueba las authorization rules y listKeys.
func TestHubAuthRules(t *testing.T) {
	srv := newTestServer(t)
	nsBase := nsURL(srv.URL, "sub1", "rg1", "testns")

	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsBase), map[string]any{
		"location": "eastus", "sku": map[string]any{"name": "Free"},
	}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsBase+"/notificationHubs/testhub"), map[string]any{
		"location": "eastus",
	}, nil)

	ruleURL := testutil.WithAPIVersion(nsBase + "/notificationHubs/testhub/authorizationRules/DefaultListenSharedAccessSignature")

	// PUT auth rule → 201
	var rule AuthRule
	status := testutil.DoJSON(t, "PUT", ruleURL, map[string]any{
		"properties": map[string]any{"rights": []string{"Listen"}},
	}, &rule)
	if status != http.StatusCreated {
		t.Fatalf("put auth rule: status=%d", status)
	}
	if len(rule.Properties.Rights) != 1 {
		t.Fatalf("rights len=%d", len(rule.Properties.Rights))
	}

	// GET auth rule → 200
	status = testutil.DoJSON(t, "GET", ruleURL, nil, &rule)
	if status != http.StatusOK {
		t.Fatalf("get auth rule: status=%d", status)
	}

	// POST listKeys → 200 with connection strings
	keysURL := testutil.WithAPIVersion(nsBase + "/notificationHubs/testhub/authorizationRules/DefaultListenSharedAccessSignature/listKeys")
	var keys map[string]any
	status = testutil.DoJSON(t, "POST", keysURL, map[string]any{}, &keys)
	if status != http.StatusOK {
		t.Fatalf("listKeys: status=%d", status)
	}
	if keys["primaryConnectionString"] == "" {
		t.Fatal("primaryConnectionString empty")
	}

	// DELETE auth rule → 204
	status = testutil.DoJSON(t, "DELETE", ruleURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete auth rule: status=%d", status)
	}
}
