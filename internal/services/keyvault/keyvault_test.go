package keyvault

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

func TestVaultLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/myvault"

	var vault Vault
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), vaultRequest{
		Location: "eastus",
		Properties: struct {
			Sku                  Sku                 `json:"sku"`
			TenantID             string              `json:"tenantId"`
			AccessPolicies       []AccessPolicyEntry `json:"accessPolicies,omitempty"`
			EnabledForDeployment bool                `json:"enabledForDeployment,omitempty"`
		}{Sku: Sku{Family: "A", Name: "standard"}, TenantID: "tenant1"},
	}, &vault)
	if status != http.StatusCreated || vault.Properties.ProvisioningState != "Succeeded" || vault.Properties.VaultURI == "" {
		t.Fatalf("put vault: status=%d vault=%+v", status, vault)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), vaultRequest{
		Location: "eastus",
		Properties: struct {
			Sku                  Sku                 `json:"sku"`
			TenantID             string              `json:"tenantId"`
			AccessPolicies       []AccessPolicyEntry `json:"accessPolicies,omitempty"`
			EnabledForDeployment bool                `json:"enabledForDeployment,omitempty"`
		}{Sku: Sku{Family: "A", Name: "standard"}, TenantID: "tenant1"},
	}, &vault)
	if status != http.StatusOK {
		t.Fatalf("update vault: want 200, got %d", status)
	}

	var got Vault
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myvault" {
		t.Fatalf("get vault: status=%d vault=%+v", status, got)
	}

	var list struct {
		Value []Vault `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list vaults: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete vault: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete vault: want 204, got %d", status)
	}
}

func TestVaultRequiresLocationSkuAndTenant(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/myvault")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing sku: want 400, got %d", status)
	}
	status = testutil.DoJSON(t, "PUT", base, map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"sku": map[string]any{"name": "standard"}},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing tenantId: want 400, got %d", status)
	}
}

func TestSecretLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/myvault.vault"

	var secret Secret
	if status := testutil.DoJSON(t, "PUT", account+"/secrets/mysecret", secretRequest{Value: "s3cr3t"}, &secret); status != 200 || secret.Value != "s3cr3t" {
		t.Fatalf("set secret: status=%d secret=%+v", status, secret)
	}

	var got Secret
	status := testutil.DoJSON(t, "GET", account+"/secrets/mysecret", nil, &got)
	if status != 200 || got.Value != "s3cr3t" {
		t.Fatalf("get secret: status=%d secret=%+v", status, got)
	}

	var list struct {
		Value []Secret `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/secrets", nil, &list)
	if status != 200 || len(list.Value) != 1 || list.Value[0].Value != "" {
		t.Fatalf("list secrets: status=%d value=%+v", status, list.Value)
	}

	r := doReq(t, http.MethodDelete, account+"/secrets/mysecret", "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("delete secret: status=%d", r.StatusCode)
	}

	status = testutil.DoJSON(t, "GET", account+"/secrets/mysecret", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted secret: want 404, got %d", status)
	}
	r = doReq(t, http.MethodDelete, account+"/secrets/mysecret", "")
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("delete missing secret: want 404 (not idempotent), got %d", r.StatusCode)
	}
}

func TestKeyLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/myvault.vault"

	var key Key
	status := testutil.DoJSON(t, "PUT", account+"/keys/mykey", keyRequest{Kty: "RSA"}, &key)
	if status != 200 || key.Key.Kty != "RSA" || key.Key.Kid == "" {
		t.Fatalf("create key: status=%d key=%+v", status, key)
	}

	var got Key
	status = testutil.DoJSON(t, "GET", account+"/keys/mykey", nil, &got)
	if status != 200 || got.Key.Kid != key.Key.Kid {
		t.Fatalf("get key: status=%d key=%+v", status, got)
	}

	var list struct {
		Value []Key `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/keys", nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list keys: status=%d value=%+v", status, list.Value)
	}

	r := doReq(t, http.MethodDelete, account+"/keys/mykey", "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("delete key: status=%d", r.StatusCode)
	}
	status = testutil.DoJSON(t, "GET", account+"/keys/mykey", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted key: want 404, got %d", status)
	}
}

func TestCertificateLifecycle(t *testing.T) {
	srv := newTestServer(t)
	account := srv.URL + "/myvault.vault"

	var cert Certificate
	status := testutil.DoJSON(t, "PUT", account+"/certificates/mycert", certificateRequest{}, &cert)
	if status != 200 || cert.Cer == "" || cert.Thumbprint == "" {
		t.Fatalf("create certificate: status=%d cert=%+v", status, cert)
	}

	var got Certificate
	status = testutil.DoJSON(t, "GET", account+"/certificates/mycert", nil, &got)
	if status != 200 || got.Thumbprint != cert.Thumbprint {
		t.Fatalf("get certificate: status=%d cert=%+v", status, got)
	}

	var list struct {
		Value []Certificate `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", account+"/certificates", nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list certificates: status=%d value=%+v", status, list.Value)
	}

	r := doReq(t, http.MethodDelete, account+"/certificates/mycert", "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("delete certificate: status=%d", r.StatusCode)
	}
	status = testutil.DoJSON(t, "GET", account+"/certificates/mycert", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted certificate: want 404, got %d", status)
	}
}

func TestInvalidVaultSuffixRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/myvault.notvault/secrets")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for wrong suffix, got %d", resp.StatusCode)
	}
}

func TestUnknownDataPlaneCollectionRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/myvault.vault/unknown")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for unknown collection, got %d", resp.StatusCode)
	}
}
