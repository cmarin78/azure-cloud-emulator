package appconfiguration

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) (*httptest.Server, *Service) {
	t.Helper()
	db := testutil.NewDB(t)
	svc := New(db)
	mux := http.NewServeMux()
	svc.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc
}

func TestStoreCRUD(t *testing.T) {
	srv, _ := newTestServer(t)

	base := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.AppConfiguration/configurationStores/my-appconfig"

	// 1. PUT create store
	reqBody := map[string]any{
		"location": "eastus",
		"sku": map[string]string{
			"name": "Standard",
		},
		"tags": map[string]string{"env": "test"},
		"properties": map[string]any{
			"publicNetworkAccess":       "Enabled",
			"purgeProtectionEnabled":    true,
			"softDeleteRetentionInDays": 7,
		},
	}

	var store ConfigurationStore
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), reqBody, &store)
	if status != http.StatusOK {
		t.Fatalf("PUT failed with status %d", status)
	}
	if store.Name != "my-appconfig" || store.Location != "eastus" || store.Sku.Name != "Standard" {
		t.Fatalf("unexpected store properties: %+v", store)
	}
	if store.Properties.Endpoint != "https://my-appconfig.azconfig.io" {
		t.Fatalf("unexpected endpoint: %q", store.Properties.Endpoint)
	}

	// 2. GET store
	var fetched ConfigurationStore
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &fetched)
	if status != http.StatusOK {
		t.Fatalf("GET failed with status %d", status)
	}
	if fetched.ID != store.ID || fetched.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("unexpected fetched store: %+v", fetched)
	}

	// 3. LIST stores
	listURL := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.AppConfiguration/configurationStores"
	var listResp struct {
		Value []ConfigurationStore `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &listResp)
	if status != http.StatusOK || len(listResp.Value) != 1 {
		t.Fatalf("LIST failed: status=%d len=%d", status, len(listResp.Value))
	}

	// 4. listKeys
	keysURL := base + "/listKeys"
	var keysResp struct {
		Value []ApiKey `json:"value"`
	}
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(keysURL), nil, &keysResp)
	if status != http.StatusOK || len(keysResp.Value) != 4 {
		t.Fatalf("listKeys failed: status=%d len=%d", status, len(keysResp.Value))
	}
	for _, key := range keysResp.Value {
		if key.ConnectionString == "" || key.Value == "" {
			t.Errorf("empty key value or connection string: %+v", key)
		}
	}

	// 5. DELETE store
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE failed with status %d", status)
	}

	// 6. GET after DELETE returns 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE expected 404, got %d", status)
	}
}

func TestStoreValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	base := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.AppConfiguration/configurationStores/store-invalid"

	// Missing location
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without location, got %d", status)
	}
}

func TestKeyValueARMCRUD(t *testing.T) {
	srv, _ := newTestServer(t)

	storeBase := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.AppConfiguration/configurationStores/cfg-store"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(storeBase), map[string]any{"location": "eastus"}, nil)

	kvBase := storeBase + "/keyValues/TestSetting$prod"

	// 1. PUT KeyValue
	putBody := map[string]any{
		"value":       "HelloAzure",
		"contentType": "text/plain",
		"tags":        map[string]string{"env": "prod"},
	}
	var kv KeyValue
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(kvBase), putBody, &kv)
	if status != http.StatusOK {
		t.Fatalf("PUT KeyValue failed: %d", status)
	}
	if kv.Key != "TestSetting" || kv.Label != "prod" || kv.Value != "HelloAzure" {
		t.Fatalf("unexpected KeyValue: %+v", kv)
	}

	// 2. GET KeyValue
	var fetched KeyValue
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(kvBase), nil, &fetched)
	if status != http.StatusOK || fetched.Value != "HelloAzure" {
		t.Fatalf("GET KeyValue failed: %d", status)
	}

	// 3. LIST KeyValues
	var listResp struct {
		Value []KeyValue `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(storeBase+"/keyValues"), nil, &listResp)
	if status != http.StatusOK || len(listResp.Value) != 1 {
		t.Fatalf("LIST KeyValues failed: status=%d len=%d", status, len(listResp.Value))
	}

	// 4. DELETE KeyValue
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(kvBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE KeyValue failed: %d", status)
	}
}

func TestKeyValueDataPlane(t *testing.T) {
	srv, svc := newTestServer(t)

	storeBase := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.AppConfiguration/configurationStores/dp-store"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(storeBase), map[string]any{"location": "eastus"}, nil)

	// Direct Data Plane test via ServeDataPlane
	// 1. PUT via DataPlane
	body := []byte(`{"value":"dp-value","content_type":"application/json"}`)
	req := httptest.NewRequest("PUT", "/kv/mykey?label=v1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	svc.ServeDataPlane(rec, req, "dp-store", "kv/mykey")
	if rec.Code != http.StatusOK {
		t.Fatalf("DataPlane PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GET via DataPlane
	reqGet := httptest.NewRequest("GET", "/kv/mykey?label=v1", nil)
	recGet := httptest.NewRecorder()
	svc.ServeDataPlane(recGet, reqGet, "dp-store", "kv/mykey")
	if recGet.Code != http.StatusOK {
		t.Fatalf("DataPlane GET failed: %d: %s", recGet.Code, recGet.Body.String())
	}

	// 3. LIST via DataPlane
	reqList := httptest.NewRequest("GET", "/kv", nil)
	recList := httptest.NewRecorder()
	svc.ServeDataPlane(recList, reqList, "dp-store", "kv")
	if recList.Code != http.StatusOK {
		t.Fatalf("DataPlane LIST failed: %d", recList.Code)
	}

	// 4. DELETE via DataPlane
	reqDel := httptest.NewRequest("DELETE", "/kv/mykey?label=v1", nil)
	recDel := httptest.NewRecorder()
	svc.ServeDataPlane(recDel, reqDel, "dp-store", "kv/mykey")
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DataPlane DELETE failed: %d", recDel.Code)
	}
}
