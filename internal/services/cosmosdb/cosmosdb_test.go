package cosmosdb

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

func TestAccountLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct"

	var acct Account
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"location": "eastus"}, &acct)
	if status != http.StatusAccepted || acct.Properties.ProvisioningState != "Succeeded" || acct.Properties.DocumentEndpoint == "" {
		t.Fatalf("put account: status=%d acct=%+v", status, acct)
	}
	if acct.Kind != "GlobalDocumentDB" {
		t.Fatalf("expected default kind GlobalDocumentDB, got %+v", acct)
	}

	var got Account
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myacct" {
		t.Fatalf("get account: status=%d acct=%+v", status, got)
	}

	var list struct {
		Value []Account `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list accounts: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete account: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete account: want 204, got %d", status)
	}
}

func TestAccountRequiresLocation(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
}

func setupAccount(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/" + name
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{"location": "eastus"}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("setup account: status=%d", status)
	}
}

func TestDatabaseLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases/mydb"

	var db Database
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, &db)
	if status != http.StatusCreated || db.Name != "mydb" {
		t.Fatalf("put database: status=%d db=%+v", status, db)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, &db)
	if status != http.StatusOK {
		t.Fatalf("update database: want 200, got %d", status)
	}

	var got Database
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "mydb" {
		t.Fatalf("get database: status=%d db=%+v", status, got)
	}

	var list struct {
		Value []Database `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list databases: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete database: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete database: want 204, got %d", status)
	}
}

func TestDatabaseRequiresExistingAccount(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/missing/sqlDatabases/mydb")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing account, got %d", status)
	}
}

func setupDatabase(t *testing.T, srv *httptest.Server, account, db string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/" + account + "/sqlDatabases/" + db
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup database: status=%d", status)
	}
}

func TestContainerLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	setupDatabase(t, srv, "myacct", "mydb")
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases/mydb/containers/mycoll"

	var c Container
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"properties": map[string]any{"resource": map[string]any{"partitionKey": map[string]any{"paths": []string{"/pk"}}}},
	}, &c)
	if status != http.StatusCreated || c.Name != "mycoll" || len(c.Properties.Resource.PartitionKey.Paths) != 1 {
		t.Fatalf("put container: status=%d c=%+v", status, c)
	}

	var got Container
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "mycoll" {
		t.Fatalf("get container: status=%d c=%+v", status, got)
	}

	var list struct {
		Value []Container `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases/mydb/containers"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list containers: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete container: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete container: want 204, got %d", status)
	}
}

func TestContainerRequiresPartitionKey(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	setupDatabase(t, srv, "myacct", "mydb")
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases/mydb/containers/mycoll")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 without partitionKey, got %d", status)
	}
}

func TestContainerRequiresExistingDatabase(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/myacct/sqlDatabases/missing/containers/mycoll")
	status := testutil.DoJSON(t, "PUT", base, map[string]any{
		"properties": map[string]any{"resource": map[string]any{"partitionKey": map[string]any{"paths": []string{"/pk"}}}},
	}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing database, got %d", status)
	}
}

func setupContainer(t *testing.T, srv *httptest.Server, account, db, container string) {
	t.Helper()
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/" + account + "/sqlDatabases/" + db + "/containers/" + container
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"properties": map[string]any{"resource": map[string]any{"partitionKey": map[string]any{"paths": []string{"/pk"}}}},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup container: status=%d", status)
	}
}

func TestDocumentLifecycle(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	setupDatabase(t, srv, "myacct", "mydb")
	setupContainer(t, srv, "myacct", "mydb", "mycoll")

	account := srv.URL + "/myacct.documents"
	docsURL := account + "/dbs/mydb/colls/mycoll/docs"

	var doc map[string]any
	status := testutil.DoJSON(t, "PUT", docsURL+"/doc1", map[string]any{"foo": "bar"}, &doc)
	if status != http.StatusCreated || doc["id"] != "doc1" || doc["foo"] != "bar" {
		t.Fatalf("put document: status=%d doc=%+v", status, doc)
	}

	status = testutil.DoJSON(t, "PUT", docsURL+"/doc1", map[string]any{"foo": "baz"}, &doc)
	if status != http.StatusOK || doc["foo"] != "baz" {
		t.Fatalf("replace document: status=%d doc=%+v", status, doc)
	}

	var got map[string]any
	status = testutil.DoJSON(t, "GET", docsURL+"/doc1", nil, &got)
	if status != 200 || got["foo"] != "baz" {
		t.Fatalf("get document: status=%d doc=%+v", status, got)
	}

	var list struct {
		Documents []map[string]any `json:"Documents"`
		Count     int              `json:"_count"`
	}
	status = testutil.DoJSON(t, "GET", docsURL, nil, &list)
	if status != 200 || list.Count != 1 || len(list.Documents) != 1 {
		t.Fatalf("list documents: status=%d list=%+v", status, list)
	}

	r := doReq(t, http.MethodDelete, docsURL+"/doc1", "")
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete document: status=%d", r.StatusCode)
	}
	status = testutil.DoJSON(t, "GET", docsURL+"/doc1", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted document: want 404, got %d", status)
	}
	r = doReq(t, http.MethodDelete, docsURL+"/doc1", "")
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("delete missing document: want 404 (not idempotent), got %d", r.StatusCode)
	}
}

func TestCreateDocumentGeneratesID(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	setupDatabase(t, srv, "myacct", "mydb")
	setupContainer(t, srv, "myacct", "mydb", "mycoll")

	docsURL := srv.URL + "/myacct.documents/dbs/mydb/colls/mycoll/docs"
	resp := doReq(t, http.MethodPost, docsURL, `{"foo":"bar"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create document: status=%d", resp.StatusCode)
	}

	var list struct {
		Documents []map[string]any `json:"Documents"`
		Count     int              `json:"_count"`
	}
	status := testutil.DoJSON(t, "GET", docsURL, nil, &list)
	if status != 200 || list.Count != 1 || list.Documents[0]["id"] == "" {
		t.Fatalf("list after create: status=%d list=%+v", status, list)
	}
}

func TestDocumentsRequireExistingContainer(t *testing.T) {
	srv := newTestServer(t)
	setupAccount(t, srv, "myacct")
	setupDatabase(t, srv, "myacct", "mydb")

	docsURL := srv.URL + "/myacct.documents/dbs/mydb/colls/missing/docs"
	status := testutil.DoJSON(t, "GET", docsURL, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing container, got %d", status)
	}
}

func TestInvalidAccountSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/myacct.notdocuments/dbs/mydb/colls/mycoll/docs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}
