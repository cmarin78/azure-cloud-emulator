package apimanagement

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

func putTestService(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ApiManagement/service/" + name
	var svc ApimService
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Developer", "capacity": 1},
		"properties": map[string]any{
			"publisherEmail": "admin@example.com",
			"publisherName":  "Contoso",
		},
	}, &svc)
	if status != http.StatusAccepted {
		t.Fatalf("put service: status=%d", status)
	}
	if svc.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put service response: %+v", svc.Properties)
	}
	if svc.Properties.GatewayURL == "" || svc.Properties.PortalURL == "" {
		t.Fatalf("expected non-empty gatewayUrl/portalUrl, got %+v", svc.Properties)
	}
	return base
}

func TestServiceLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := putTestService(t, srv, "myapim")

	var got ApimService
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myapim" {
		t.Fatalf("get service: status=%d service=%+v", status, got)
	}

	var list struct {
		Value []ApimService `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ApiManagement/service"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list services: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete service: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete service: want 204, got %d", status)
	}
}

func TestServiceRequiresPublisherFields(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ApiManagement/service/myapim"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for missing publisher fields, got %d", status)
	}
}

func TestAPIAndOperationLifecycle(t *testing.T) {
	srv := newTestServer(t)
	svcBase := putTestService(t, srv, "myapim")

	apiBase := svcBase + "/apis/echo"
	var api Api
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(apiBase), map[string]any{
		"properties": map[string]any{
			"displayName": "Echo API",
			"path":        "echo",
			"serviceUrl":  "https://backend.example.com",
		},
	}, &api)
	if status != http.StatusCreated || api.Properties.DisplayName != "Echo API" {
		t.Fatalf("put api: status=%d api=%+v", status, api)
	}
	if len(api.Properties.Protocols) != 1 || api.Properties.Protocols[0] != "https" {
		t.Fatalf("expected default https protocol, got %+v", api.Properties.Protocols)
	}

	opBase := apiBase + "/operations/get-echo"
	var op ApiOperation
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(opBase), map[string]any{
		"properties": map[string]any{
			"displayName": "GET echo",
			"method":      "get",
			"urlTemplate": "/{id}",
		},
	}, &op)
	if status != http.StatusCreated || op.Properties.Method != "GET" {
		t.Fatalf("put operation: status=%d op=%+v", status, op)
	}

	var opList struct {
		Value []ApiOperation `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(apiBase+"/operations"), nil, &opList)
	if status != http.StatusOK || len(opList.Value) != 1 {
		t.Fatalf("list operations: status=%d value=%+v", status, opList.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(apiBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete api: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(opBase), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected operation gone after parent api delete, got %d", status)
	}
}

func TestAPIRequiresExistingService(t *testing.T) {
	srv := newTestServer(t)
	apiBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ApiManagement/service/missing/apis/echo"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(apiBase), map[string]any{
		"properties": map[string]any{"displayName": "Echo API", "path": "echo"},
	}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing parent service, got %d", status)
	}
}

func TestProductAndSubscriptionLifecycle(t *testing.T) {
	srv := newTestServer(t)
	svcBase := putTestService(t, srv, "myapim")

	apiBase := svcBase + "/apis/echo"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(apiBase), map[string]any{
		"properties": map[string]any{"displayName": "Echo API", "path": "echo"},
	}, nil)

	productBase := svcBase + "/products/starter"
	var product Product
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(productBase), map[string]any{
		"properties": map[string]any{
			"displayName": "Starter",
		},
	}, &product)
	if status != http.StatusCreated || product.Properties.State != "published" {
		t.Fatalf("put product: status=%d product=%+v", status, product)
	}

	productAPIBase := productBase + "/apis/echo"
	var linkedAPI Api
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(productAPIBase), nil, &linkedAPI)
	if status != http.StatusOK || linkedAPI.Name != "echo" {
		t.Fatalf("put product api association: status=%d api=%+v", status, linkedAPI)
	}

	var productAPIs struct {
		Value []Api `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(productBase+"/apis"), nil, &productAPIs)
	if status != http.StatusOK || len(productAPIs.Value) != 1 {
		t.Fatalf("list product apis: status=%d value=%+v", status, productAPIs.Value)
	}

	subBase := svcBase + "/subscriptions/starter-sub"
	var sub ApimSubscription
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(subBase), map[string]any{
		"properties": map[string]any{
			"displayName": "Starter subscription",
			"scope":       productBase,
		},
	}, &sub)
	if status != http.StatusCreated || sub.Properties.State != "active" {
		t.Fatalf("put subscription: status=%d sub=%+v", status, sub)
	}
	if sub.Properties.PrimaryKey == "" || sub.Properties.SecondaryKey == "" {
		t.Fatalf("expected non-empty fake keys, got %+v", sub.Properties)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(subBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete subscription: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(productBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete product: status=%d", status)
	}
}

func TestServiceDeleteCascadesSubResources(t *testing.T) {
	srv := newTestServer(t)
	svcBase := putTestService(t, srv, "myapim")

	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(svcBase+"/apis/echo"), map[string]any{
		"properties": map[string]any{"displayName": "Echo API", "path": "echo"},
	}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(svcBase+"/products/starter"), map[string]any{
		"properties": map[string]any{"displayName": "Starter"},
	}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(svcBase+"/subscriptions/starter-sub"), map[string]any{
		"properties": map[string]any{"displayName": "Starter subscription", "scope": svcBase + "/products/starter"},
	}, nil)

	status := testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(svcBase), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete service: status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(svcBase+"/apis/echo"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected api gone after service delete, got %d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(svcBase+"/products/starter"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected product gone after service delete, got %d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(svcBase+"/subscriptions/starter-sub"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected subscription gone after service delete, got %d", status)
	}
}
