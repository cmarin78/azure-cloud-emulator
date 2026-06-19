package blob

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

func TestContainerAndBlobLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.blob"

	// Create container.
	req, _ := http.NewRequest(http.MethodPut, account+"/mycontainer?restype=container", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create container: status=%d", resp.StatusCode)
	}

	// Duplicate create -> 409.
	req, _ = http.NewRequest(http.MethodPut, account+"/mycontainer?restype=container", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dup create container: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup create container: want 409, got %d", resp.StatusCode)
	}

	// Upload blob.
	req, _ = http.NewRequest(http.MethodPut, account+"/mycontainer/hello.txt", strings.NewReader("hello world"))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put blob: status=%d", resp.StatusCode)
	}

	// Download blob.
	resp, err = http.Get(account + "/mycontainer/hello.txt")
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	body := make([]byte, 32)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body[:n]) != "hello world" {
		t.Fatalf("get blob: status=%d body=%q", resp.StatusCode, body[:n])
	}

	// List blobs.
	var list struct {
		Value []Blob `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/mycontainer?restype=container&comp=list", nil, &list)
	if status != 200 || len(list.Value) != 1 || list.Value[0].Name != "hello.txt" {
		t.Fatalf("list blobs: status=%d value=%+v", status, list.Value)
	}

	// Delete blob.
	req, _ = http.NewRequest(http.MethodDelete, account+"/mycontainer/hello.txt", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete blob: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete blob: status=%d", resp.StatusCode)
	}

	// Delete container.
	req, _ = http.NewRequest(http.MethodDelete, account+"/mycontainer?restype=container", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete container: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete container: status=%d", resp.StatusCode)
	}
}

func TestListContainersAtAccountLevel(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.blob"

	req, _ := http.NewRequest(http.MethodPut, account+"/c1?restype=container", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	var list struct {
		Value []Container `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/?comp=list", nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list containers: status=%d value=%+v", status, list.Value)
	}
}

func TestGetMissingBlobReturns404(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.blob"
	req, _ := http.NewRequest(http.MethodPut, account+"/c1?restype=container", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	resp, err := http.Get(account + "/c1/missing.txt")
	if err != nil {
		t.Fatalf("get missing blob: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestInvalidAccountSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/acct1.notblob/c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}
