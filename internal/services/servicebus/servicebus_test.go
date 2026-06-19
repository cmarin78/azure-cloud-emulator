package servicebus

import (
	"encoding/json"
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
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns"

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
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces"
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
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func setupNamespace(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/" + name
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), namespaceRequest{
		Location: "eastus",
		Sku:      Sku{Name: "Standard"},
	}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("setup namespace: status=%d", status)
	}
}

func TestQueueLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/queues/myqueue"

	var q Queue
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"properties": map[string]any{"maxSizeInMegabytes": 1024}}, &q)
	if status != http.StatusCreated || q.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put queue: status=%d q=%+v", status, q)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, &q)
	if status != http.StatusOK {
		t.Fatalf("update queue: want 200, got %d", status)
	}

	var got Queue
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myqueue" {
		t.Fatalf("get queue: status=%d q=%+v", status, got)
	}

	var list struct {
		Value []Queue `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/queues"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list queues: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete queue: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete queue: want 204, got %d", status)
	}
}

func TestQueueRequiresExistingNamespace(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/missing/queues/myqueue")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing namespace, got %d", status)
	}
}

func TestTopicAndSubscriptionLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	topicBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/topics/mytopic"

	var topic Topic
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(topicBase), map[string]any{}, &topic)
	if status != http.StatusCreated || topic.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put topic: status=%d topic=%+v", status, topic)
	}

	subBase := topicBase + "/subscriptions/mysub"
	var sub Subscription
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(subBase), map[string]any{}, &sub)
	if status != http.StatusCreated || sub.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put subscription: status=%d sub=%+v", status, sub)
	}

	var got Subscription
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(subBase), nil, &got)
	if status != 200 || got.Name != "mysub" {
		t.Fatalf("get subscription: status=%d sub=%+v", status, got)
	}

	var list struct {
		Value []Subscription `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(topicBase+"/subscriptions"), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list subscriptions: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(subBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete subscription: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(topicBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete topic: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(topicBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete topic: want 204, got %d", status)
	}
}

func TestQueueMessagingLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	arm := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/queues/myqueue"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(arm), map[string]any{}, nil)

	account := srv.URL + "/myns.servicebus"

	resp := doReq(t, http.MethodPost, account+"/myqueue/messages", "hello")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send message: status=%d", resp.StatusCode)
	}

	var recv struct {
		Value []Message `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/myqueue/messages", nil, &recv)
	if status != 200 || len(recv.Value) != 1 || recv.Value[0].Body != "hello" || recv.Value[0].LockToken == "" {
		t.Fatalf("receive message: status=%d value=%+v", status, recv.Value)
	}
	msg := recv.Value[0]

	resp = doReq(t, http.MethodDelete, account+"/myqueue/messages/"+msg.MessageID+"?lockToken="+msg.LockToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("complete message: status=%d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodDelete, account+"/myqueue/messages/"+msg.MessageID+"?lockToken="+msg.LockToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("complete already-completed message: want 404, got %d", resp.StatusCode)
	}
}

func TestPeekLockDoesNotConsumeMessage(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	arm := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/queues/q1"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(arm), map[string]any{}, nil)
	account := srv.URL + "/myns.servicebus"
	doReq(t, http.MethodPost, account+"/q1/messages", "msg1").Body.Close()

	var peek struct {
		Value []Message `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/q1/messages?peeklock=false", nil, &peek)
	if status != 200 || len(peek.Value) != 1 || peek.Value[0].LockToken != "" {
		t.Fatalf("peek: status=%d value=%+v", status, peek.Value)
	}

	var recv struct {
		Value []Message `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/q1/messages", nil, &recv)
	if status != 200 || len(recv.Value) != 1 {
		t.Fatalf("receive after peek: status=%d value=%+v", status, recv.Value)
	}
}

func TestCompleteMessageWrongLockTokenRejected(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	arm := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/queues/q1"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(arm), map[string]any{}, nil)
	account := srv.URL + "/myns.servicebus"
	doReq(t, http.MethodPost, account+"/q1/messages", "msg1").Body.Close()

	var recv struct {
		Value []Message `json:"value"`
	}
	testutil.DoJSON(t, "GET", account+"/q1/messages", nil, &recv)
	msg := recv.Value[0]

	resp := doReq(t, http.MethodDelete, account+"/q1/messages/"+msg.MessageID+"?lockToken=wrongtoken", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 on lockToken mismatch, got %d", resp.StatusCode)
	}
}

func TestTopicFanOutToSubscriptions(t *testing.T) {
	srv := newTestServer(t)
	setupNamespace(t, srv, "myns")
	topicBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/myns/topics/mytopic"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(topicBase), map[string]any{}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(topicBase+"/subscriptions/subA"), map[string]any{}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(topicBase+"/subscriptions/subB"), map[string]any{}, nil)

	account := srv.URL + "/myns.servicebus"
	resp := doReq(t, http.MethodPost, account+"/mytopic/messages", "fanout")
	var sendResult struct {
		DeliveredTo int       `json:"deliveredTo"`
		Value       []Message `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sendResult); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send to topic: status=%d", resp.StatusCode)
	}
	if sendResult.DeliveredTo != 2 {
		t.Fatalf("send to topic: want deliveredTo=2, got %+v", sendResult)
	}

	for _, sub := range []string{"subA", "subB"} {
		var recv struct {
			Value []Message `json:"value"`
		}
		status := testutil.DoJSON(t, "GET", account+"/mytopic/subscriptions/"+sub+"/messages", nil, &recv)
		if status != 200 || len(recv.Value) != 1 || recv.Value[0].Body != "fanout" {
			t.Fatalf("receive from subscription %s: status=%d value=%+v", sub, status, recv.Value)
		}
	}
}

func TestInvalidNamespaceSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/myns.notservicebus/myqueue/messages")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}
