package graph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type spList struct {
	Value []servicePrincipal `json:"value"`
}

type appList struct {
	Value []application `json:"value"`
}

func TestListServicePrincipalsByAppID(t *testing.T) {
	srv := newTestServer(t)

	filter := url.QueryEscape("appId eq 'app-123'")
	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + filter)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 1 || list.Value[0].AppID != "app-123" || list.Value[0].ID == "" {
		t.Fatalf("list: %+v", list.Value)
	}
}

func TestListServicePrincipalsIsDeterministic(t *testing.T) {
	srv := newTestServer(t)

	filter := url.QueryEscape("appId eq 'app-456'")
	getOnce := func() string {
		resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + filter)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		var list spList
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(list.Value) != 1 {
			t.Fatalf("list: %+v", list.Value)
		}
		return list.Value[0].ID
	}

	id1 := getOnce()
	id2 := getOnce()
	if id1 != id2 {
		t.Fatalf("expected stable object id across calls, got %q and %q", id1, id2)
	}
}

func TestListServicePrincipalsWithoutFilterReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 0 {
		t.Fatalf("expected empty list, got %+v", list.Value)
	}
}

func TestListServicePrincipalsWithUnparseableFilterReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + url.QueryEscape("unsupported filter"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 0 {
		t.Fatalf("expected empty list for unparseable filter, got %+v", list.Value)
	}
}

func TestApplicationLifecycle(t *testing.T) {
	srv := newTestServer(t)

	var app application
	status := testutil.DoJSON(t, "POST", srv.URL+"/v1.0/applications", map[string]any{
		"displayName": "phase15-demo-app",
	}, &app)
	if status != http.StatusCreated || app.ID == "" || app.AppID == "" || app.SignInAudience != "AzureADMyOrg" {
		t.Fatalf("create application: status=%d app=%+v", status, app)
	}

	var got application
	status = testutil.DoJSON(t, "GET", srv.URL+"/v1.0/applications/"+app.ID, nil, &got)
	if status != http.StatusOK || got.ID != app.ID || got.DisplayName != "phase15-demo-app" {
		t.Fatalf("get application: status=%d got=%+v", status, got)
	}

	var list appList
	status = testutil.DoJSON(t, "GET", srv.URL+"/v1.0/applications", nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list applications: status=%d list=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", srv.URL+"/v1.0/applications/"+app.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete application: status=%d", status)
	}
	// idempotente
	status = testutil.DoJSON(t, "DELETE", srv.URL+"/v1.0/applications/"+app.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete application (again): status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", srv.URL+"/v1.0/applications/"+app.ID, nil, &got)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
}

func TestApplicationCreateRequiresDisplayName(t *testing.T) {
	srv := newTestServer(t)

	status := testutil.DoJSON(t, "POST", srv.URL+"/v1.0/applications", map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestServicePrincipalExplicitCreateMatchesAutoDiscovery(t *testing.T) {
	srv := newTestServer(t)

	var sp servicePrincipal
	status := testutil.DoJSON(t, "POST", srv.URL+"/v1.0/servicePrincipals", map[string]any{
		"appId":       "app-789",
		"displayName": "phase15-demo-sp",
	}, &sp)
	if status != http.StatusCreated || sp.ID == "" || sp.DisplayName != "phase15-demo-sp" {
		t.Fatalf("create service principal: status=%d sp=%+v", status, sp)
	}

	var got servicePrincipal
	status = testutil.DoJSON(t, "GET", srv.URL+"/v1.0/servicePrincipals/"+sp.ID, nil, &got)
	if status != http.StatusOK || got.DisplayName != "phase15-demo-sp" {
		t.Fatalf("get service principal by id: status=%d got=%+v", status, got)
	}

	// El descubrimiento por $filter debe devolver el mismo objeto persistido
	// (mismo ID determinista, displayName real en vez del genérico).
	filter := url.QueryEscape("appId eq 'app-789'")
	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + filter)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 1 || list.Value[0].ID != sp.ID || list.Value[0].DisplayName != "phase15-demo-sp" {
		t.Fatalf("expected discovery to return persisted sp, got %+v", list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", srv.URL+"/v1.0/servicePrincipals/"+sp.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete service principal: status=%d", status)
	}
}

func TestServicePrincipalCreateRequiresAppID(t *testing.T) {
	srv := newTestServer(t)

	status := testutil.DoJSON(t, "POST", srv.URL+"/v1.0/servicePrincipals", map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}
