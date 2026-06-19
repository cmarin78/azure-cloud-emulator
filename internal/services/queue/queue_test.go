package queue

import (
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
	svc := New(db)
	mux.HandleFunc("/{accountResource}/{path...}", svc.ServeHTTP)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doReq(t *testing.T, method, url string, body string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestQueueAndMessageLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.queue"

	resp := doReq(t, http.MethodPut, account+"/myqueue", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create queue: status=%d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodPut, account+"/myqueue", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup create queue: want 409, got %d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodPost, account+"/myqueue/messages", "hello")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put message: status=%d", resp.StatusCode)
	}

	var getResult struct {
		Value []Message `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/myqueue/messages?numofmessages=10", nil, &getResult)
	if status != 200 || len(getResult.Value) != 1 || getResult.Value[0].MessageText != "hello" {
		t.Fatalf("get messages: status=%d value=%+v", status, getResult.Value)
	}
	msg := getResult.Value[0]
	if msg.PopReceipt == "" {
		t.Fatalf("expected popReceipt on dequeue")
	}

	resp = doReq(t, http.MethodDelete, account+"/myqueue/messages/"+msg.ID+"?popreceipt="+msg.PopReceipt, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete message: status=%d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodDelete, account+"/myqueue", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete queue: status=%d", resp.StatusCode)
	}
}

func TestPeekDoesNotDequeue(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.queue"
	doReq(t, http.MethodPut, account+"/q1", "").Body.Close()
	doReq(t, http.MethodPost, account+"/q1/messages", "msg1").Body.Close()

	var peek struct {
		Value []Message `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/q1/messages?peekonly=true", nil, &peek)
	if status != 200 || len(peek.Value) != 1 || peek.Value[0].PopReceipt != "" {
		t.Fatalf("peek: status=%d value=%+v", status, peek.Value)
	}

	// Message should still be available for a real dequeue afterward.
	var deq struct {
		Value []Message `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/q1/messages", nil, &deq)
	if status != 200 || len(deq.Value) != 1 {
		t.Fatalf("dequeue after peek: status=%d value=%+v", status, deq.Value)
	}
}

func TestDeleteMessageWrongPopReceiptRejected(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.queue"
	doReq(t, http.MethodPut, account+"/q1", "").Body.Close()
	doReq(t, http.MethodPost, account+"/q1/messages", "msg1").Body.Close()

	var deq struct {
		Value []Message `json:"value"`
	}
	testutil.DoJSON(t, "GET", account+"/q1/messages", nil, &deq)
	msg := deq.Value[0]

	resp := doReq(t, http.MethodDelete, account+"/q1/messages/"+msg.ID+"?popreceipt=wrong", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 on popReceipt mismatch, got %d", resp.StatusCode)
	}
}
