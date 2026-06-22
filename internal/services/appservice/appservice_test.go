package appservice

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

func TestPlanLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/myplan"

	var plan AppServicePlan
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"kind":     "linux",
		"sku":      map[string]any{"name": "B1", "tier": "Basic"},
	}, &plan)
	if status != http.StatusCreated || plan.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put plan: status=%d plan=%+v", status, plan)
	}
	if !plan.Properties.Reserved {
		t.Fatalf("expected reserved=true for kind=linux, got %+v", plan.Properties)
	}
	if plan.Sku.Capacity != 1 {
		t.Fatalf("expected default sku.capacity=1, got %+v", plan.Sku)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "B1"},
	}, &plan)
	if status != http.StatusOK {
		t.Fatalf("update plan: want 200, got %d", status)
	}

	var got AppServicePlan
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myplan" {
		t.Fatalf("get plan: status=%d plan=%+v", status, got)
	}

	var list struct {
		Value []AppServicePlan `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list plans: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete plan: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete plan: want 204, got %d", status)
	}
}

func TestPlanRequiresLocationAndSkuName(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/myplan")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing sku.name: want 400, got %d", status)
	}
}

func TestSiteLifecycle(t *testing.T) {
	srv := newTestServer(t)
	planID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/myplan"
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/mysite"

	var site Site
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"kind":     "app,linux",
		"properties": map[string]any{
			"serverFarmId": planID,
			"siteConfig":   map[string]any{"linuxFxVersion": "DOCKER|nginx:latest"},
		},
	}, &site)
	if status != http.StatusCreated || site.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put site: status=%d site=%+v", status, site)
	}
	if site.Properties.State != "Running" || !site.Properties.Enabled {
		t.Fatalf("expected new site to default to Running/enabled, got %+v", site.Properties)
	}
	if site.Properties.DefaultHostName != "mysite.azurewebsites.net" {
		t.Fatalf("unexpected defaultHostName: %q", site.Properties.DefaultHostName)
	}
	if site.Properties.SiteConfig.LinuxFxVersion != "DOCKER|nginx:latest" {
		t.Fatalf("expected linuxFxVersion to round-trip, got %+v", site.Properties.SiteConfig)
	}

	var got Site
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "mysite" {
		t.Fatalf("get site: status=%d site=%+v", status, got)
	}

	var list struct {
		Value []Site `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list sites: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/stop"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("stop site: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Properties.State != "Stopped" {
		t.Fatalf("expected state=Stopped after stop, got %+v", got.Properties)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/start"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("start site: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Properties.State != "Running" {
		t.Fatalf("expected state=Running after start, got %+v", got.Properties)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/restart"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("restart site: status=%d", status)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete site: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete site: want 204, got %d", status)
	}
}

// TestSiteSystemAssignedIdentity cubre Phase 16: un site con
// identity.type "SystemAssigned" debe devolver un principalId/tenantId
// deterministas (no vacíos) y estables entre PUTs, mismo comportamiento
// que aks.TestManagedClusterLifecycle ya verifica para AKS.
func TestSiteSystemAssignedIdentity(t *testing.T) {
	srv := newTestServer(t)
	planID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/myplan"
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/mysite"

	body := map[string]any{
		"location": "eastus",
		"identity": map[string]any{"type": "SystemAssigned"},
		"properties": map[string]any{
			"serverFarmId": planID,
		},
	}

	var site Site
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &site)
	if status != http.StatusCreated {
		t.Fatalf("put site with identity: status=%d", status)
	}
	if site.Identity == nil || site.Identity.Type != "SystemAssigned" || site.Identity.PrincipalID == "" || site.Identity.TenantID == "" {
		t.Fatalf("expected a populated SystemAssigned identity, got %+v", site.Identity)
	}

	var again Site
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &again)
	if status != http.StatusOK {
		t.Fatalf("put site again: status=%d", status)
	}
	if again.Identity.PrincipalID != site.Identity.PrincipalID || again.Identity.TenantID != site.Identity.TenantID {
		t.Fatalf("expected deterministic identity across PUTs, got %+v vs %+v", site.Identity, again.Identity)
	}
}

// TestSiteWithoutIdentityOmitsIdentity cubre el caso por defecto (sin
// bloque "identity" en el request): el campo debe quedar nil.
func TestSiteWithoutIdentityOmitsIdentity(t *testing.T) {
	srv := newTestServer(t)
	planID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/myplan"
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/mysite"

	var site Site
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"serverFarmId": planID},
	}, &site)
	if status != http.StatusCreated {
		t.Fatalf("put site: status=%d", status)
	}
	if site.Identity != nil {
		t.Fatalf("expected nil identity when not requested, got %+v", site.Identity)
	}
}

func TestSiteRequiresLocationAndServerFarmID(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/mysite")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing properties.serverFarmId: want 400, got %d", status)
	}
}

func TestSiteActionsRequireExistingSite(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/missing"

	for _, action := range []string{"start", "stop", "restart"} {
		status := testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/"+action), nil, nil)
		if status != http.StatusNotFound {
			t.Fatalf("%s on missing site: want 404, got %d", action, status)
		}
	}
}

func TestAppSettingsLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/mysite/config/appsettings"

	// GET antes de cualquier PUT: debe devolver un diccionario vacío, no 404
	// (mismo "auto-vivify" que documenta putAppSettings).
	var empty struct {
		Properties map[string]string `json:"properties"`
	}
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &empty)
	if status != http.StatusOK || len(empty.Properties) != 0 {
		t.Fatalf("get appsettings before put: status=%d body=%+v", status, empty)
	}

	var got struct {
		Properties map[string]string `json:"properties"`
	}
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"properties": map[string]string{"WEBSITES_PORT": "8080", "ENV": "prod"},
	}, &got)
	if status != http.StatusOK || got.Properties["WEBSITES_PORT"] != "8080" || got.Properties["ENV"] != "prod" {
		t.Fatalf("put appsettings: status=%d body=%+v", status, got)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || len(got.Properties) != 2 {
		t.Fatalf("get appsettings after put: status=%d body=%+v", status, got)
	}

	// El siguiente PUT reemplaza el diccionario completo (no hace merge).
	// Usa una variable nueva: json.Decode sobre un map[string]string ya
	// poblado (como "got") agrega/sobreescribe claves en vez de vaciar el
	// mapa primero -- es el decoder de Go, no el servicio, el que "mergea"
	// si se reutiliza la misma variable de salida.
	var replaced struct {
		Properties map[string]string `json:"properties"`
	}
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"properties": map[string]string{"ENV": "staging"},
	}, &replaced)
	if status != http.StatusOK || len(replaced.Properties) != 1 || replaced.Properties["ENV"] != "staging" {
		t.Fatalf("put appsettings (replace): status=%d body=%+v", status, replaced)
	}
}
