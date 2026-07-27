package storageaccounts

import (
	"net/http"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

const (
	testSubFS  = "sub-fs"
	testRGFS   = "rg-fs"
	testAcctFS = "acctfs"
)

func fileServiceBaseURL(srvURL string) string {
	return srvURL + "/subscriptions/" + testSubFS +
		"/resourceGroups/" + testRGFS +
		"/providers/Microsoft.Storage/storageAccounts/" + testAcctFS +
		"/fileServices"
}

func fileShareBaseURL(srvURL, share string) string {
	return fileServiceBaseURL(srvURL) + "/default/shares/" + share
}

// TestFileServiceGet verifica que GET .../fileServices devuelve la lista
// y que GET .../fileServices/default devuelve el recurso raíz.
func TestFileServiceGet(t *testing.T) {
	srv := newTestServer(t)
	base := fileServiceBaseURL(srv.URL)

	// LIST
	var list struct {
		Value []FileServiceResource `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list fileServices: status=%d", status)
	}
	if len(list.Value) != 1 || list.Value[0].Name != "default" {
		t.Fatalf("list fileServices: unexpected value=%+v", list.Value)
	}

	// GET /default
	var svc FileServiceResource
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/default"), nil, &svc)
	if status != http.StatusOK {
		t.Fatalf("get fileService: status=%d", status)
	}
	if svc.Name != "default" || svc.Type != "Microsoft.Storage/storageAccounts/fileServices" {
		t.Fatalf("get fileService: unexpected=%+v", svc)
	}
	if svc.ID == "" {
		t.Fatal("get fileService: ID empty")
	}
}

// TestFileShareCRUD prueba el ciclo PUT → GET → LIST → DELETE para un share.
func TestFileShareCRUD(t *testing.T) {
	srv := newTestServer(t)
	shareURL := fileShareBaseURL(srv.URL, "myshare")
	listURL := fileServiceBaseURL(srv.URL) + "/default/shares"

	type shareBody struct {
		Properties struct {
			ShareQuota       int    `json:"shareQuota"`
			EnabledProtocols string `json:"enabledProtocols"`
			AccessTier       string `json:"accessTier"`
		} `json:"properties"`
	}

	// PUT (create) → 201
	body := shareBody{}
	body.Properties.ShareQuota = 100
	body.Properties.EnabledProtocols = "SMB"
	body.Properties.AccessTier = "Hot"

	var created FileShare
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(shareURL), body, &created)
	if status != http.StatusCreated {
		t.Fatalf("put (create): status=%d", status)
	}
	if created.Name != "myshare" {
		t.Fatalf("put: unexpected name %q", created.Name)
	}
	if created.Properties.ShareQuota != 100 {
		t.Fatalf("put: shareQuota=%d, want 100", created.Properties.ShareQuota)
	}
	if created.Properties.EnabledProtocols != "SMB" {
		t.Fatalf("put: enabledProtocols=%q, want SMB", created.Properties.EnabledProtocols)
	}
	if created.Properties.AccessTier != "Hot" {
		t.Fatalf("put: accessTier=%q, want Hot", created.Properties.AccessTier)
	}
	if created.ID == "" {
		t.Fatal("put: ID empty")
	}

	// GET → 200
	var got FileShare
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(shareURL), nil, &got)
	if status != http.StatusOK {
		t.Fatalf("get: status=%d", status)
	}
	if got.Properties.ShareQuota != 100 {
		t.Fatalf("get: shareQuota=%d, want 100", got.Properties.ShareQuota)
	}

	// PUT idempotente (update) → 200
	body.Properties.ShareQuota = 200
	var updated FileShare
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(shareURL), body, &updated)
	if status != http.StatusOK {
		t.Fatalf("put (update): status=%d", status)
	}
	if updated.Properties.ShareQuota != 200 {
		t.Fatalf("put (update): shareQuota=%d, want 200", updated.Properties.ShareQuota)
	}

	// LIST → 1 item
	var list struct {
		Value []FileShare `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list: status=%d", status)
	}
	if len(list.Value) != 1 {
		t.Fatalf("list: want 1 item, got %d", len(list.Value))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(shareURL), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status=%d", status)
	}

	// GET después de borrar → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(shareURL), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: want 404, got %d", status)
	}

	// DELETE idempotente → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(shareURL), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete idempotente: status=%d", status)
	}
}

// TestFileShareDefaults verifica que los campos opcionales toman sus valores
// por defecto cuando no se envían en el PUT.
func TestFileShareDefaults(t *testing.T) {
	srv := newTestServer(t)
	shareURL := fileShareBaseURL(srv.URL, "defaultshare")

	// PUT sin quota, protocol ni tier
	type minBody struct {
		Properties struct{} `json:"properties"`
	}

	var sh FileShare
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(shareURL), minBody{}, &sh)
	if status != http.StatusCreated {
		t.Fatalf("put defaults: status=%d", status)
	}
	if sh.Properties.ShareQuota != 5120 {
		t.Fatalf("default quota: want 5120, got %d", sh.Properties.ShareQuota)
	}
	if sh.Properties.EnabledProtocols != "SMB" {
		t.Fatalf("default protocol: want SMB, got %q", sh.Properties.EnabledProtocols)
	}
	if sh.Properties.AccessTier != "TransactionOptimized" {
		t.Fatalf("default tier: want TransactionOptimized, got %q", sh.Properties.AccessTier)
	}
}

// TestFileShareMultiAccount verifica que los shares de dos cuentas distintas
// no se mezclan en el LIST.
func TestFileShareMultiAccount(t *testing.T) {
	srv := newTestServer(t)

	url1 := srv.URL + "/subscriptions/sub1/resourceGroups/rg1" +
		"/providers/Microsoft.Storage/storageAccounts/acct1" +
		"/fileServices/default/shares/share-a"
	url2 := srv.URL + "/subscriptions/sub1/resourceGroups/rg1" +
		"/providers/Microsoft.Storage/storageAccounts/acct2" +
		"/fileServices/default/shares/share-b"
	list1 := srv.URL + "/subscriptions/sub1/resourceGroups/rg1" +
		"/providers/Microsoft.Storage/storageAccounts/acct1" +
		"/fileServices/default/shares"

	type shareBody struct {
		Properties struct {
			ShareQuota int `json:"shareQuota"`
		} `json:"properties"`
	}
	body := shareBody{}
	body.Properties.ShareQuota = 50

	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(url1), body, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(url2), body, nil)

	var list struct {
		Value []FileShare `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(list1), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list acct1: status=%d", status)
	}
	if len(list.Value) != 1 {
		t.Fatalf("list acct1: want 1 item, got %d (shares leaked between accounts)", len(list.Value))
	}
	if list.Value[0].Name != "share-a" {
		t.Fatalf("list acct1: expected share-a, got %q", list.Value[0].Name)
	}
}
