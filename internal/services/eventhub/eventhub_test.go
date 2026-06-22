package eventhub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	ops := server.NewOperations()
	mux := http.NewServeMux()
	svc := New(db, ops)
	svc.Register(mux)
	mux.HandleFunc("/{accountResource}/{path...}", svc.ServeHTTP)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestNamespaceLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns"

	var ns Namespace
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), namespaceRequest{
		Location: "eastus",
		Sku:      Sku{Name: "Standard"},
	}, &ns)
	if status != http.StatusAccepted || ns.Properties.ProvisioningState != "Succeeded" || ns.Properties.ServiceBusEndpoint == "" {
		t.Fatalf("put namespace: status=%d ns=%+v", status, ns)
	}

	var got Namespace
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myns" {
		t.Fatalf("get namespace: status=%d ns=%+v", status, got)
	}

	var list struct {
		Value []Namespace `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list namespaces: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete namespace: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete namespace: want 204, got %d", status)
	}
}

func TestNamespaceRequiresLocation(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func setupNamespace(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/" + name
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), namespaceRequest{
		Location: "eastus",
		Sku:      Sku{Name: "Standard"},
	}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("setup namespace: status=%d", status)
	}
}

func TestEventHubLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns/eventhubs/myhub"

	var hub EventHubResource
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"properties": map[string]any{"partitionCount": 2}}, &hub)
	if status != http.StatusCreated || hub.Properties.ProvisioningState != "Succeeded" || hub.Properties.PartitionCount != 2 || len(hub.Properties.PartitionIds) != 2 {
		t.Fatalf("put hub: status=%d hub=%+v", status, hub)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, &hub)
	if status != http.StatusOK {
		t.Fatalf("update hub: want 200, got %d", status)
	}
	if hub.Properties.PartitionCount != 4 {
		t.Fatalf("default partitionCount: want 4, got %d", hub.Properties.PartitionCount)
	}

	var got EventHubResource
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myhub" {
		t.Fatalf("get hub: status=%d hub=%+v", status, got)
	}

	var list struct {
		Value []EventHubResource `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns/eventhubs"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list hubs: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete hub: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete hub: want 204, got %d", status)
	}
}

func TestEventHubRequiresExistingNamespace(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/missing/eventhubs/myhub")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing namespace, got %d", status)
	}
}

func setupHub(t *testing.T, srv *httptest.Server, ns, hub string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/" + ns + "/eventhubs/" + hub
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup hub: status=%d", status)
	}
}

func TestConsumerGroupLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	setupHub(t, srv, "myns", "myhub")
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns/eventhubs/myhub/consumergroups/mycg"

	var cg ConsumerGroup
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, &cg)
	if status != http.StatusCreated || cg.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put consumer group: status=%d cg=%+v", status, cg)
	}

	var got ConsumerGroup
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "mycg" {
		t.Fatalf("get consumer group: status=%d cg=%+v", status, got)
	}

	var list struct {
		Value []ConsumerGroup `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns/eventhubs/myhub/consumergroups"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list consumer groups: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete consumer group: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete consumer group: want 204, got %d", status)
	}
}

func TestConsumerGroupRequiresExistingHub(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.EventHub/namespaces/myns/eventhubs/missing/consumergroups/mycg")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing hub, got %d", status)
	}
}

func TestSendReceiveLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	setupHub(t, srv, "myns", "myhub")
	account := srv.URL + "/myns.eventhub"

	resp := doReq(t, http.MethodPost, account+"/myhub/messages", "event1")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send event: status=%d", resp.StatusCode)
	}
	resp = doReq(t, http.MethodPost, account+"/myhub/messages", "event2")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send event: status=%d", resp.StatusCode)
	}

	var recv struct {
		Value []storedEvent `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/myhub/messages", nil, &recv)
	if status != 200 || len(recv.Value) != 2 {
		t.Fatalf("receive events: status=%d value=%+v", status, recv.Value)
	}
	if recv.Value[0].Offset != 0 || recv.Value[0].Body != "event1" {
		t.Fatalf("first event: %+v", recv.Value[0])
	}
	if recv.Value[1].Offset != 1 || recv.Value[1].Body != "event2" {
		t.Fatalf("second event: %+v", recv.Value[1])
	}

	var fromOffset struct {
		Value []storedEvent `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/myhub/messages?offset=0", nil, &fromOffset)
	if status != 200 || len(fromOffset.Value) != 1 || fromOffset.Value[0].Offset != 1 {
		t.Fatalf("receive from offset: status=%d value=%+v", status, fromOffset.Value)
	}

	var viaConsumerGroup struct {
		Value []storedEvent `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/myhub/consumergroups/$Default/messages", nil, &viaConsumerGroup)
	if status != 200 || len(viaConsumerGroup.Value) != 2 {
		t.Fatalf("receive via consumer group: status=%d value=%+v", status, viaConsumerGroup.Value)
	}
}

func TestSendRequiresExistingHub(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	account := srv.URL + "/myns.eventhub"
	resp := doReq(t, http.MethodPost, account+"/missing/messages", "event1")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for missing hub, got %d", resp.StatusCode)
	}
}

func TestInvalidNamespaceSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/myns.noteventhub/myhub/messages")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}
