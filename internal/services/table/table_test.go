package table

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

func doReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestTableAndEntityLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.table"

	resp := doReq(t, http.MethodPost, account+"/Tables", `{"TableName":"mytable"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create table: status=%d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodPost, account+"/Tables", `{"TableName":"mytable"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup create table: want 409, got %d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodPost, account+"/mytable", `{"PartitionKey":"pk1","RowKey":"rk1","Foo":"bar"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("insert entity: status=%d", resp.StatusCode)
	}

	var entity map[string]any
	status := testutil.DoJSON(t, "GET", account+"/mytable(PartitionKey='pk1',RowKey='rk1')", nil, &entity)
	if status != 200 || entity["Foo"] != "bar" {
		t.Fatalf("get entity: status=%d entity=%+v", status, entity)
	}

	resp = doReq(t, http.MethodPut, account+"/mytable(PartitionKey='pk1',RowKey='rk1')", `{"Foo":"baz","Extra":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("replace entity: status=%d", resp.StatusCode)
	}

	var replaced map[string]any
	testutil.DoJSON(t, "GET", account+"/mytable(PartitionKey='pk1',RowKey='rk1')", nil, &replaced)
	if replaced["Foo"] != "baz" {
		t.Fatalf("replace did not take effect: %+v", replaced)
	}

	resp = doReq(t, http.MethodDelete, account+"/mytable(PartitionKey='pk1',RowKey='rk1')", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete entity: status=%d", resp.StatusCode)
	}

	resp = doReq(t, http.MethodDelete, account+"/Tables('mytable')", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete table: status=%d", resp.StatusCode)
	}
}

func TestQueryEntitiesFilter(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.table"
	doReq(t, http.MethodPost, account+"/Tables", `{"TableName":"t1"}`).Body.Close()
	doReq(t, http.MethodPost, account+"/t1", `{"PartitionKey":"pk1","RowKey":"rk1"}`).Body.Close()
	doReq(t, http.MethodPost, account+"/t1", `{"PartitionKey":"pk2","RowKey":"rk2"}`).Body.Close()

	var all struct {
		Value []map[string]any `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", account+"/t1()", nil, &all)
	if status != 200 || len(all.Value) != 2 {
		t.Fatalf("query all: status=%d value=%+v", status, all.Value)
	}

	var filtered struct {
		Value []map[string]any `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/t1()?$filter="+url_QueryEscape("PartitionKey eq 'pk1'"), nil, &filtered)
	if status != 200 || len(filtered.Value) != 1 {
		t.Fatalf("query filtered: status=%d value=%+v", status, filtered.Value)
	}
}

func TestEntityAlreadyExistsConflict(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/acct1.table"
	doReq(t, http.MethodPost, account+"/Tables", `{"TableName":"t1"}`).Body.Close()
	doReq(t, http.MethodPost, account+"/t1", `{"PartitionKey":"pk1","RowKey":"rk1"}`).Body.Close()

	resp := doReq(t, http.MethodPost, account+"/t1", `{"PartitionKey":"pk1","RowKey":"rk1"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup insert entity: want 409, got %d", resp.StatusCode)
	}
}

// url_QueryEscape avoids importing net/url just for one call site.
func url_QueryEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteString("%20")
		case r == '\'':
			b.WriteString("%27")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
