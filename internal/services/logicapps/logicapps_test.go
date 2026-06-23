package logicapps

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func recurrenceDefinition(uri string, frequency string, interval int) map[string]any {
	def := map[string]any{
		"$schema":        "https://schema.management.azure.com/providers/Microsoft.Logic/schemas/2016-06-01/workflowdefinition.json#",
		"contentVersion": "1.0.0.0",
		"triggers": map[string]any{
			"recurrence": map[string]any{
				"type": "Recurrence",
				"recurrence": map[string]any{
					"frequency": frequency,
					"interval":  interval,
				},
			},
		},
	}
	if uri != "" {
		def["actions"] = map[string]any{
			"callEndpoint": map[string]any{
				"type": "Http",
				"inputs": map[string]any{
					"method": "POST",
					"uri":    uri,
				},
			},
		}
	}
	return def
}

func TestWorkflowLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	def := recurrenceDefinition("https://example.invalid/hook", "Hour", 1)

	var wf Workflow
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": def,
		},
	}, &wf)
	if status != http.StatusCreated || wf.Properties.State != "Enabled" || wf.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put workflow: status=%d wf=%+v", status, wf)
	}
	if wf.Properties.CreatedTime == "" {
		t.Fatalf("expected createdTime to be set, got %+v", wf.Properties)
	}
	firstCreated := wf.Properties.CreatedTime

	// Update: createdTime debe permanecer estable, changedTime debe avanzar.
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": def,
		},
	}, &wf)
	if status != http.StatusOK || wf.Properties.CreatedTime != firstCreated {
		t.Fatalf("update workflow: status=%d wf=%+v (createdTime should stay stable)", status, wf)
	}

	var got Workflow
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "myworkflow" {
		t.Fatalf("get workflow: status=%d wf=%+v", status, got)
	}

	var list struct {
		Value []Workflow `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list workflows: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete workflow: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete workflow: want 204, got %d", status)
	}
}

func TestWorkflowRequiresLocationAndDefinition(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{
		"properties": map[string]any{"definition": map[string]any{}},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}

	status = testutil.DoJSON(t, "PUT", base, map[string]any{
		"location": "eastus",
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing definition: want 400, got %d", status)
	}
}

func TestWorkflowTriggerRunDispatchesRealHTTP(t *testing.T) {
	received := make(chan struct{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	var wf Workflow
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": recurrenceDefinition(receiver.URL, "Day", 1),
		},
	}, &wf)
	if status != http.StatusCreated {
		t.Fatalf("put workflow: status=%d", status)
	}

	var ran Workflow
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/triggers/recurrence/run"), map[string]any{}, &ran)
	if status != http.StatusOK {
		t.Fatalf("run trigger: status=%d", status)
	}
	if ran.Properties.LastRunStatus != "ok" || ran.Properties.LastRunTime == "" {
		t.Fatalf("expected lastRun* to be recorded, got %+v", ran.Properties)
	}

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("Http action never received a real HTTP call")
	}
}

func TestWorkflowTriggerRunRequiresExistingTrigger(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": recurrenceDefinition("", "Day", 1),
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put workflow: status=%d", status)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/triggers/doesNotExist/run"), map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("run nonexistent trigger: want 404, got %d", status)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(srv.URL+"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/missing/triggers/recurrence/run"), map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("run trigger on missing workflow: want 404, got %d", status)
	}
}

func TestListAndGetTrigger(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": recurrenceDefinition("", "Day", 1),
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put workflow: status=%d", status)
	}

	var list struct {
		Value []triggerInfo `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/triggers"), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 || list.Value[0].Name != "recurrence" {
		t.Fatalf("list triggers: status=%d value=%+v", status, list.Value)
	}

	var trig triggerInfo
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/triggers/recurrence"), nil, &trig)
	if status != http.StatusOK || trig.Type != "Recurrence" {
		t.Fatalf("get trigger: status=%d trig=%+v", status, trig)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/triggers/missing"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get missing trigger: want 404, got %d", status)
	}
}

func TestRecurrenceFiresAutomatically(t *testing.T) {
	received := make(chan struct{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	def := recurrenceDefinition(receiver.URL, "Second", 1)
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": def,
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put workflow: status=%d", status)
	}

	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("recurrence trigger never fired automatically")
	}
}

func TestDisabledWorkflowDoesNotFire(t *testing.T) {
	received := make(chan struct{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	def := recurrenceDefinition(receiver.URL, "Second", 1)
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"state":      "Disabled",
			"definition": def,
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("put workflow: status=%d", status)
	}

	select {
	case <-received:
		t.Fatal("a Disabled workflow should never fire automatically")
	case <-time.After(3 * time.Second):
	}
}

func TestNonRecurrenceTriggerIsPassthroughOnly(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Logic/workflows/myworkflow"

	def := map[string]any{
		"triggers": map[string]any{
			"manual": map[string]any{
				"type": "Request",
				"kind": "Http",
			},
		},
	}
	var wf Workflow
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"definition": def,
		},
	}, &wf)
	if status != http.StatusCreated || wf.Properties.State != "Enabled" {
		t.Fatalf("put workflow with non-Recurrence trigger: status=%d wf=%+v (should still be accepted)", status, wf)
	}

	var raw json.RawMessage
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &wf)
	if status != http.StatusOK {
		t.Fatalf("get workflow: status=%d", status)
	}
	_ = raw
}
