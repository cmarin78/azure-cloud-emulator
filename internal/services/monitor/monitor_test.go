package monitor

import (
	"encoding/json"
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

func TestWorkspaceLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/myworkspace"

	var ws Workspace
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
	}, &ws)
	if status != http.StatusCreated || ws.Properties.ProvisioningState != "Succeeded" || ws.Properties.CustomerID == "" {
		t.Fatalf("put workspace: status=%d ws=%+v", status, ws)
	}
	if ws.Properties.Sku.Name != "PerGB2018" || ws.Properties.RetentionInDays != 30 {
		t.Fatalf("expected workspace defaults, got %+v", ws.Properties)
	}
	firstCustomerID := ws.Properties.CustomerID

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
	}, &ws)
	if status != http.StatusOK || ws.Properties.CustomerID != firstCustomerID {
		t.Fatalf("update workspace: status=%d ws=%+v (customerId should stay stable)", status, ws)
	}

	var got Workspace
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myworkspace" {
		t.Fatalf("get workspace: status=%d ws=%+v", status, got)
	}

	var list struct {
		Value []Workspace `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list workspaces: status=%d value=%+v", status, list.Value)
	}

	var keys map[string]string
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/sharedKeys"), nil, &keys)
	if status != http.StatusOK || keys["primarySharedKey"] == "" || keys["secondarySharedKey"] == "" || keys["primarySharedKey"] == keys["secondarySharedKey"] {
		t.Fatalf("shared keys: status=%d keys=%+v", status, keys)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete workspace: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete workspace: want 204, got %d", status)
	}
}

func TestWorkspaceRequiresLocation(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/myworkspace")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func TestWorkspaceSharedKeysRequiresExistingWorkspace(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/missing/sharedKeys"

	status := testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("sharedKeys on missing workspace: want 404, got %d", status)
	}
}

func TestActionGroupLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/actionGroups/myactiongroup"

	var ag ActionGroup
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "global",
		"properties": map[string]any{
			"groupShortName": "shortname",
			"emailReceivers": []map[string]any{
				{"name": "admin", "emailAddress": "admin@example.com"},
			},
		},
	}, &ag)
	if status != http.StatusCreated || !ag.Properties.Enabled || ag.Properties.GroupShortName != "shortname" {
		t.Fatalf("put action group: status=%d ag=%+v", status, ag)
	}
	if len(ag.Properties.EmailReceivers) == 0 {
		t.Fatalf("expected emailReceivers to round-trip, got %+v", ag.Properties)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "global",
		"properties": map[string]any{
			"groupShortName": "shortname",
		},
	}, &ag)
	if status != http.StatusOK {
		t.Fatalf("update action group: want 200, got %d", status)
	}

	var got ActionGroup
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myactiongroup" {
		t.Fatalf("get action group: status=%d ag=%+v", status, got)
	}

	var list struct {
		Value []ActionGroup `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/actionGroups"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list action groups: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete action group: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete action group: want 204, got %d", status)
	}
}

func TestActionGroupRequiresLocationAndShortName(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/actionGroups/myactiongroup")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "global"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing groupShortName: want 400, got %d", status)
	}
}

func TestMetricAlertLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/metricAlerts/myalert"

	var alert MetricAlert
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "global",
		"properties": map[string]any{
			"scopes": []string{"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/myvm"},
			"criteria": map[string]any{
				"odata.type": "Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria",
			},
		},
	}, &alert)
	if status != http.StatusCreated || !alert.Properties.Enabled || alert.Properties.Severity != 3 {
		t.Fatalf("put metric alert: status=%d alert=%+v", status, alert)
	}
	if alert.Properties.EvaluationFrequency != "PT5M" || alert.Properties.WindowSize != "PT15M" {
		t.Fatalf("expected default frequency/window, got %+v", alert.Properties)
	}
	if len(alert.Properties.Criteria) == 0 {
		t.Fatalf("expected criteria to round-trip, got %+v", alert.Properties)
	}

	var got MetricAlert
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myalert" {
		t.Fatalf("get metric alert: status=%d alert=%+v", status, got)
	}

	var list struct {
		Value []MetricAlert `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/metricAlerts"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list metric alerts: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete metric alert: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete metric alert: want 204, got %d", status)
	}
}

func TestMetricAlertRequiresLocationAndScopes(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Insights/metricAlerts/myalert")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "global"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing scopes: want 400, got %d", status)
	}
}

func TestQueryWorkspaceStub(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Post(srv.URL+"/v1/workspaces/abc-123/query", "application/json",
		strings.NewReader(`{"query":"AzureActivity | take 1"}`))
	if err != nil {
		t.Fatalf("post query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query workspace: want 200, got %d", resp.StatusCode)
	}

	var body struct {
		Tables []struct {
			Name    string `json:"name"`
			Columns []any  `json:"columns"`
			Rows    []any  `json:"rows"`
		} `json:"tables"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	if len(body.Tables) != 1 || body.Tables[0].Name != "PrimaryResult" || len(body.Tables[0].Rows) != 0 {
		t.Fatalf("expected one empty PrimaryResult table, got %+v", body.Tables)
	}
}
