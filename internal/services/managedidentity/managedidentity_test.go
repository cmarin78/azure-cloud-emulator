package managedidentity

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

// TestUserAssignedIdentityLifecycle cubre el ARM CRUD síncrono completo:
// PUT (create), PUT (update idempotente, preservando tenantId/principalId/
// clientId entre actualizaciones), GET, LIST y DELETE (idempotente, 204 si
// ya no existe) -- mismo patrón que appservice.TestPlanLifecycle.
func TestUserAssignedIdentityLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myid"

	var identity UserAssignedIdentity
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "test"},
	}, &identity)
	if status != http.StatusCreated {
		t.Fatalf("put identity: status=%d identity=%+v", status, identity)
	}
	if identity.Properties.TenantID == "" || identity.Properties.PrincipalID == "" || identity.Properties.ClientID == "" {
		t.Fatalf("expected non-empty deterministic tenantId/principalId/clientId, got %+v", identity.Properties)
	}
	if identity.Name != "myid" || identity.Type != "Microsoft.ManagedIdentity/userAssignedIdentities" {
		t.Fatalf("unexpected identity shape: %+v", identity)
	}

	// Un segundo PUT (update) debe ser idempotente en el sentido de
	// preservar los valores deterministas ya generados, no "rotarlos".
	var updated UserAssignedIdentity
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"tags":     map[string]string{"env": "prod"},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("update identity: want 200, got %d", status)
	}
	if updated.Properties.TenantID != identity.Properties.TenantID ||
		updated.Properties.PrincipalID != identity.Properties.PrincipalID ||
		updated.Properties.ClientID != identity.Properties.ClientID {
		t.Fatalf("expected deterministic properties to survive update, got %+v vs %+v", identity.Properties, updated.Properties)
	}
	if updated.Tags["env"] != "prod" {
		t.Fatalf("expected tags to update, got %+v", updated.Tags)
	}

	var got UserAssignedIdentity
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myid" {
		t.Fatalf("get identity: status=%d identity=%+v", status, got)
	}

	var list struct {
		Value []UserAssignedIdentity `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list identities: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete identity: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete identity: want 204, got %d", status)
	}
}

func TestUserAssignedIdentityRequiresLocation(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myid")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func TestUserAssignedIdentityNotFound(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/missing"
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404, got %d", status)
	}
}
