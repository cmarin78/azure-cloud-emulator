package appservice

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func staticSiteURL(srvURL, sub, rg, name string) string {
	return srvURL + "/subscriptions/" + sub +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.Web/staticSites/" + name
}

// TestStaticSiteCRUD prueba el ciclo PUT → GET → LIST → DELETE.
func TestStaticSiteCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := staticSiteURL(srv.URL, "sub1", "rg1", "mystaticsite")
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/staticSites"

	// PUT (create) → 201
	var created StaticSite
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus2",
		"sku":      map[string]any{"name": "Free"},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("put (create): status=%d", status)
	}
	if created.Name != "mystaticsite" {
		t.Fatalf("put: unexpected name %q", created.Name)
	}
	if created.Properties.DefaultHostname == "" {
		t.Fatal("put: defaultHostname is empty")
	}
	if !strings.HasSuffix(created.Properties.DefaultHostname, ".azurestaticapps.net") {
		t.Fatalf("put: defaultHostname %q doesn't end with .azurestaticapps.net", created.Properties.DefaultHostname)
	}
	if created.Sku.Name != "Free" {
		t.Fatalf("put: sku.name=%q, want Free", created.Sku.Name)
	}
	if created.ID == "" {
		t.Fatal("put: ID empty")
	}

	// PUT idempotente (update) → 200
	var updated StaticSite
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus2",
		"sku":      map[string]any{"name": "Standard"},
	}, &updated)
	if status != http.StatusOK {
		t.Fatalf("put (update): status=%d", status)
	}
	if updated.Sku.Name != "Standard" {
		t.Fatalf("put (update): sku.name=%q, want Standard", updated.Sku.Name)
	}

	// GET → 200
	var got StaticSite
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK {
		t.Fatalf("get: status=%d", status)
	}
	if got.Name != "mystaticsite" {
		t.Fatalf("get: unexpected name %q", got.Name)
	}

	// LIST → 1 item
	var list struct {
		Value []StaticSite `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list: status=%d", status)
	}
	if len(list.Value) != 1 {
		t.Fatalf("list: want 1 item, got %d", len(list.Value))
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

// TestStaticSiteListSecrets verifica POST .../listSecrets devuelve apiKey.
func TestStaticSiteListSecrets(t *testing.T) {
	srv := newTestServer(t)
	base := staticSiteURL(srv.URL, "sub1", "rg1", "secretsite")
	secretsURL := testutil.WithAPIVersion(base + "/listSecrets")

	// listSecrets en site inexistente → 404
	status := testutil.DoJSON(t, "POST", secretsURL, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("listSecrets on nonexistent site: want 404, got %d", status)
	}

	// Crear site
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus2",
	}, nil)

	// listSecrets → 200 con apiKey
	var secrets StaticSiteSecrets
	status = testutil.DoJSON(t, "POST", secretsURL, nil, &secrets)
	if status != http.StatusOK {
		t.Fatalf("listSecrets: status=%d", status)
	}
	if secrets.Properties.APIKey == "" {
		t.Fatal("listSecrets: apiKey is empty")
	}
	if !strings.HasPrefix(secrets.Properties.APIKey, "fake-api-key-for-secretsite") {
		t.Fatalf("listSecrets: unexpected apiKey %q", secrets.Properties.APIKey)
	}
}

// TestStaticSiteDefaults verifica los valores por defecto cuando no se envía sku.
func TestStaticSiteDefaults(t *testing.T) {
	srv := newTestServer(t)
	base := staticSiteURL(srv.URL, "sub1", "rg1", "defaultsite")

	var site StaticSite
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus2",
	}, &site)
	if status != http.StatusCreated {
		t.Fatalf("put defaults: status=%d", status)
	}
	if site.Sku.Name != "Free" {
		t.Fatalf("default sku: want Free, got %q", site.Sku.Name)
	}
	if site.Properties.DefaultHostname != "defaultsite.azurestaticapps.net" {
		t.Fatalf("default hostname: want defaultsite.azurestaticapps.net, got %q",
			site.Properties.DefaultHostname)
	}
}

// TestStaticSiteAPIKeyStable verifica que listSecrets devuelve el mismo
// apiKey en llamadas repetidas (sin drift en plan de Terraform).
func TestStaticSiteAPIKeyStable(t *testing.T) {
	srv := newTestServer(t)
	base := staticSiteURL(srv.URL, "sub1", "rg1", "stablesite")
	secretsURL := testutil.WithAPIVersion(base + "/listSecrets")

	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus2",
	}, nil)

	var s1, s2 StaticSiteSecrets
	testutil.DoJSON(t, "POST", secretsURL, nil, &s1)
	testutil.DoJSON(t, "POST", secretsURL, nil, &s2)

	if s1.Properties.APIKey != s2.Properties.APIKey {
		t.Fatalf("apiKey not stable: first=%q second=%q",
			s1.Properties.APIKey, s2.Properties.APIKey)
	}
}
