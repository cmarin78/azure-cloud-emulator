package functions

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

func TestFunctionLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/myfuncapp"

	var def FunctionDefinition
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), map[string]any{
		"properties": map[string]any{
			"language": "python",
			"config": map[string]any{
				"bindings": []map[string]any{
					{"type": "httpTrigger", "direction": "in", "authLevel": "function"},
				},
			},
		},
	}, &def)
	if status != http.StatusCreated {
		t.Fatalf("put function: status=%d", status)
	}
	if def.Properties.InvokeURLTemplate == "" {
		t.Fatalf("expected a non-empty invoke_url_template, got %+v", def.Properties)
	}
	if len(def.Properties.Config) == 0 {
		t.Fatalf("expected config to be persisted, got %+v", def.Properties)
	}

	// Un segundo PUT sobre la misma función actualiza (200), no crea (201).
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), map[string]any{
		"properties": map[string]any{
			"config": map[string]any{"bindings": []map[string]any{}},
		},
	}, &def)
	if status != http.StatusOK {
		t.Fatalf("update function: status=%d", status)
	}

	var got FunctionDefinition
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), nil, &got)
	if status != http.StatusOK || got.Properties.Name != "HttpTrigger1" {
		t.Fatalf("get function: status=%d def=%+v", status, got)
	}

	var list struct {
		Value []FunctionDefinition `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/functions"), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list functions: status=%d value=%+v", status, list.Value)
	}

	// syncfunctiontriggers es una acción sync sin cuerpo (204).
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/syncfunctiontriggers"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("syncfunctiontriggers: status=%d", status)
	}

	// host/default/listkeys debe devolver una masterKey no vacía.
	var keys struct {
		MasterKey string `json:"masterKey"`
	}
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/host/default/listkeys"), nil, &keys)
	if status != http.StatusOK || keys.MasterKey == "" {
		t.Fatalf("listkeys: status=%d keys=%+v", status, keys)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete function: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base+"/functions/HttpTrigger1"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete function: want 204, got %d", status)
	}
}

func TestFunctionRequiresConfig(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/myfuncapp"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/functions/Broken"), map[string]any{
		"properties": map[string]any{"language": "python"},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for missing config, got %d", status)
	}
}

// TestFunctionNoParentValidation confirma el "no-validación de referencias
// cruzadas" documentado en putFunction: crear una función bajo un site que
// nunca fue creado vía Microsoft.Web/sites igual funciona, mismo enfoque
// que metricAlerts/actionGroupId en monitor.
func TestFunctionNoParentValidation(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Web/sites/never-created"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/functions/Foo"), map[string]any{
		"properties": map[string]any{"config": map[string]any{"bindings": []map[string]any{}}},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("expected put to succeed without a parent site, got %d", status)
	}
}
