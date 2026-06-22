package eventgrid

import (
	"bytes"
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
	svc := New(db)
	svc.Register(mux)
	mux.HandleFunc("/{accountResource}/{path...}", svc.ServeHTTP)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTopicLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics/mytopic"

	var topic Topic
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), topicRequest{Location: "eastus"}, &topic)
	if status != http.StatusCreated || topic.Properties.ProvisioningState != "Succeeded" || topic.Properties.Endpoint == "" || topic.Properties.InputSchema != "EventGridSchema" {
		t.Fatalf("put topic: status=%d topic=%+v", status, topic)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), topicRequest{Location: "eastus"}, &topic)
	if status != http.StatusOK {
		t.Fatalf("update topic: want 200, got %d", status)
	}

	var got Topic
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "mytopic" {
		t.Fatalf("get topic: status=%d topic=%+v", status, got)
	}

	var list struct {
		Value []Topic `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list topics: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete topic: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete topic: want 204, got %d", status)
	}
}

func TestTopicRequiresLocation(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics/mytopic")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func setupTopic(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics/" + name
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), topicRequest{Location: "eastus"}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup topic: status=%d", status)
	}
}

func eventSubscriptionBase(srv *httptest.Server, topic, name string) string {
	return srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics/" + topic +
		"/providers/Microsoft.EventGrid/eventSubscriptions/" + name
}

func TestEventSubscriptionLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupTopic(t, srv, "mytopic")
	base := eventSubscriptionBase(srv, "mytopic", "mysub")

	body := map[string]any{
		"properties": map[string]any{
			"destination": map[string]any{
				"endpointType": "WebHook",
				"properties":   map[string]any{"endpointUrl": "http://example.invalid/hook"},
			},
		},
	}

	var es EventSubscription
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &es)
	if status != http.StatusCreated || es.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put event subscription: status=%d es=%+v", status, es)
	}

	var got EventSubscription
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "mysub" {
		t.Fatalf("get event subscription: status=%d es=%+v", status, got)
	}

	var list struct {
		Value []EventSubscription `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventGrid/topics/mytopic/providers/Microsoft.EventGrid/eventSubscriptions"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list event subscriptions: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete event subscription: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete event subscription: want 204, got %d", status)
	}
}

func TestEventSubscriptionRequiresExistingTopic(t *testing.T) {
	srv := newTestServer(t)
	base := eventSubscriptionBase(srv, "missing", "mysub")
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"properties": map[string]any{"destination": map[string]any{}},
	}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing topic, got %d", status)
	}
}

func TestEventSubscriptionRequiresDestination(t *testing.T) {
	srv := newTestServer(t)
	setupTopic(t, srv, "mytopic")
	base := eventSubscriptionBase(srv, "mytopic", "mysub")
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"properties": map[string]any{}}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing destination: want 400, got %d", status)
	}
}

// TestPublishDispatchesRealWebhook verifica el camino completo: publicar un
// evento en el topic dispara de verdad un POST HTTP al endpoint webhook de
// la event subscription (no solo se simula), y el resultado se refleja en
// LastDeliveryStatus -- igual que el test análogo de Action Groups en la
// Fase 20.
func TestPublishDispatchesRealWebhook(t *testing.T) {
	received := make(chan string, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload) > 0 {
			if subj, ok := payload[0]["subject"].(string); ok {
				received <- subj
			} else {
				received <- "ok"
			}
		} else {
			received <- "ok"
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	srv := newTestServer(t)
	setupTopic(t, srv, "mytopic")
	subBase := eventSubscriptionBase(srv, "mytopic", "mysub")
	body := map[string]any{
		"properties": map[string]any{
			"destination": map[string]any{
				"endpointType": "WebHook",
				"properties":   map[string]any{"endpointUrl": hook.URL},
			},
		},
	}
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(subBase), body, nil)
	if status != http.StatusCreated {
		t.Fatalf("put event subscription: status=%d", status)
	}

	publishURL := srv.URL + "/mytopic.eventgrid/api/events"
	events := []map[string]any{{"subject": "/test/subject", "eventType": "Test.Event", "data": map[string]any{"x": 1}}}
	payload, _ := json.Marshal(events)
	resp, err := http.Post(publishURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: want 200, got %d", resp.StatusCode)
	}

	select {
	case subj := <-received:
		if subj != "/test/subject" {
			t.Fatalf("webhook received unexpected payload: subject=%q", subj)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook was not called within timeout")
	}

	// Espera a que dispatchToSubscription termine de persistir el estado
	// (corre en su propia goroutine, lanzada después de que el handler de
	// publish ya respondió) antes de leer LastDeliveryStatus.
	var es EventSubscription
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		testutil.DoJSON(t, "GET", testutil.WithAPIVersion(subBase), nil, &es)
		if es.Properties.LastDeliveryStatus != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if es.Properties.LastDeliveryStatus != "ok" {
		t.Fatalf("want LastDeliveryStatus=ok, got %+v", es.Properties)
	}
}

func TestPublishRequiresExistingTopic(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Post(srv.URL+"/missing.eventgrid/api/events", "application/json", bytes.NewReader([]byte("[]")))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for missing topic, got %d", resp.StatusCode)
	}
}

func TestInvalidTopicSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/mytopic.nottopic/api/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}
