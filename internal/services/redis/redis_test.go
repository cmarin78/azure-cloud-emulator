package redis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mux := http.NewServeMux()
	New(db).Register(mux)
	return httptest.NewServer(mux)
}

const (
	sub  = "00000000-0000-0000-0000-000000000000"
	rg   = "test-rg"
	name = "mytestcache"
	api  = "2023-08-01"
)

func cacheURL(srv *httptest.Server, suffix string) string {
	base := srv.URL + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.Cache/redis"
	if suffix == "" {
		return base + "?api-version=" + api
	}
	return base + "/" + suffix + "?api-version=" + api
}

func TestRedisCacheCRUD(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	// Formato az CLI: sku de nivel superior (backward-compat).
	body := `{"location":"eastus","sku":{"name":"Standard","family":"C","capacity":1}}`

	// PUT (create)
	resp, err := http.DefaultClient.Do(req(t, "PUT", cacheURL(srv, name), body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT want 201 got %d", resp.StatusCode)
	}

	// GET
	resp, err = http.DefaultClient.Do(req(t, "GET", cacheURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	var c RedisCache
	json.NewDecoder(resp.Body).Decode(&c)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET want 200 got %d", resp.StatusCode)
	}
	if c.Properties.HostName != name+".redis.cache.windows.net" {
		t.Errorf("HostName = %q", c.Properties.HostName)
	}
	if c.Properties.SslPort != 6380 {
		t.Errorf("SslPort = %d", c.Properties.SslPort)
	}
	if c.Properties.AccessKeys == nil || c.Properties.AccessKeys.PrimaryKey == "" {
		t.Error("AccessKeys should be populated in GET response")
	}
	// Sku debe estar dentro de properties (go-azure-sdk / azurerm provider lo
	// lee de model.Properties.Sku, no de un campo de nivel superior).
	if c.Properties.Sku.Name != "Standard" {
		t.Errorf("Sku.Name = %q, want Standard", c.Properties.Sku.Name)
	}
	if c.Properties.Sku.Capacity != 1 {
		t.Errorf("Sku.Capacity = %d, want 1", c.Properties.Sku.Capacity)
	}

	// PUT (idempotent update)
	resp, err = http.DefaultClient.Do(req(t, "PUT", cacheURL(srv, name), body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT update want 200 got %d", resp.StatusCode)
	}

	// LIST
	resp, err = http.DefaultClient.Do(req(t, "GET", cacheURL(srv, ""), ""))
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Value []RedisCache }
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Value) != 1 {
		t.Fatalf("LIST want 1 item got %d", len(list.Value))
	}

	// listKeys
	keysURL := srv.URL + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.Cache/redis/" + name + "/listKeys?api-version=" + api
	resp, err = http.DefaultClient.Do(req(t, "POST", keysURL, "{}"))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]string
	json.NewDecoder(resp.Body).Decode(&keys)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listKeys want 200 got %d", resp.StatusCode)
	}
	if keys["primaryKey"] == "" {
		t.Error("primaryKey should not be empty")
	}
	if keys["primaryConnectionString"] == "" {
		t.Error("primaryConnectionString should not be empty")
	}
	// claves deterministas: dos llamadas deben devolver el mismo valor
	resp2, _ := http.DefaultClient.Do(req(t, "POST", keysURL, "{}"))
	var keys2 map[string]string
	json.NewDecoder(resp2.Body).Decode(&keys2)
	resp2.Body.Close()
	if keys["primaryKey"] != keys2["primaryKey"] {
		t.Error("primaryKey should be deterministic")
	}

	// DELETE
	resp, err = http.DefaultClient.Do(req(t, "DELETE", cacheURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE want 200 got %d", resp.StatusCode)
	}

	// GET after delete → 404
	resp, err = http.DefaultClient.Do(req(t, "GET", cacheURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete want 404 got %d", resp.StatusCode)
	}

	// DELETE idempotent (ya no existe → 204)
	resp, err = http.DefaultClient.Do(req(t, "DELETE", cacheURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE missing want 204 got %d", resp.StatusCode)
	}
}

// TestRedisCacheProviderFormat verifica que el formato go-azure-sdk
// (sku dentro de properties) también se decodifica correctamente.
func TestRedisCacheProviderFormat(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	// Formato azurerm provider: sku dentro de properties (RedisCreateParameters).
	body := `{"location":"eastus","properties":{"sku":{"name":"Premium","family":"P","capacity":2},"enableNonSslPort":false,"minimumTlsVersion":"1.2"}}`

	resp, err := http.DefaultClient.Do(req(t, "PUT", cacheURL(srv, "providerformatcache"), body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT want 201 got %d", resp.StatusCode)
	}

	resp, err = http.DefaultClient.Do(req(t, "GET", cacheURL(srv, "providerformatcache"), ""))
	if err != nil {
		t.Fatal(err)
	}
	var c RedisCache
	json.NewDecoder(resp.Body).Decode(&c)
	resp.Body.Close()

	if c.Properties.Sku.Name != "Premium" {
		t.Errorf("Sku.Name = %q, want Premium", c.Properties.Sku.Name)
	}
	if c.Properties.Sku.Capacity != 2 {
		t.Errorf("Sku.Capacity = %d, want 2", c.Properties.Sku.Capacity)
	}
}

func req(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
	} else {
		r, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}
