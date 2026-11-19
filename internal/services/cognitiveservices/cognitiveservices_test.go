package cognitiveservices

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) (*httptest.Server, *Service) {
	t.Helper()
	db := testutil.NewDB(t)
	svc := New(db)
	mux := http.NewServeMux()
	svc.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc
}

func TestAccountCRUD(t *testing.T) {
	srv, _ := newTestServer(t)

	base := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.CognitiveServices/accounts/my-openai"

	// 1. PUT create account (OpenAI)
	putBody := map[string]any{
		"location": "eastus",
		"kind":     "OpenAI",
		"sku": map[string]string{
			"name": "S0",
		},
		"tags": map[string]string{"env": "test"},
		"properties": map[string]any{
			"customSubDomainName": "my-openai",
			"publicNetworkAccess": "Enabled",
		},
	}

	var account CognitiveAccount
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), putBody, &account)
	if status != http.StatusOK {
		t.Fatalf("PUT failed with status %d", status)
	}
	if account.Name != "my-openai" || account.Kind != "OpenAI" || account.Sku.Name != "S0" {
		t.Fatalf("unexpected account properties: %+v", account)
	}
	if account.Properties.Endpoint != "https://my-openai.openai.azure.com/" {
		t.Fatalf("unexpected endpoint: %q", account.Properties.Endpoint)
	}

	// 2. GET account
	var fetched CognitiveAccount
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &fetched)
	if status != http.StatusOK {
		t.Fatalf("GET failed with status %d", status)
	}
	if fetched.ID != account.ID || fetched.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("unexpected fetched account: %+v", fetched)
	}

	// 3. LIST accounts
	listURL := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.CognitiveServices/accounts"
	var listResp struct {
		Value []CognitiveAccount `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &listResp)
	if status != http.StatusOK || len(listResp.Value) != 1 {
		t.Fatalf("LIST failed: status=%d len=%d", status, len(listResp.Value))
	}

	// 4. listKeys
	keysURL := base + "/listKeys"
	var keysResp AccountKeys
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(keysURL), nil, &keysResp)
	if status != http.StatusOK || keysResp.Key1 == "" || keysResp.Key2 == "" {
		t.Fatalf("listKeys failed: status=%d keys=%+v", status, keysResp)
	}

	// 5. DELETE account
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE failed with status %d", status)
	}

	// 6. GET after DELETE returns 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE expected 404, got %d", status)
	}
}

func TestAccountValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	base := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.CognitiveServices/accounts/bad-acc"

	// Missing location
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without location, got %d", status)
	}
}

func TestDeploymentCRUD(t *testing.T) {
	srv, _ := newTestServer(t)

	accBase := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.CognitiveServices/accounts/acc-dep"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(accBase), map[string]any{"location": "eastus"}, nil)

	depBase := accBase + "/deployments/gpt-4o-model"

	// 1. PUT Deployment
	putBody := map[string]any{
		"sku": map[string]any{
			"name":     "Standard",
			"capacity": 10,
		},
		"properties": map[string]any{
			"model": map[string]string{
				"format":  "OpenAI",
				"name":    "gpt-4o",
				"version": "2024-05-13",
			},
			"raiPolicyName":        "Microsoft.Default",
			"versionUpgradeOption": "OnceNewDefaultVersionAvailable",
		},
	}

	var dep CognitiveDeployment
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(depBase), putBody, &dep)
	if status != http.StatusOK {
		t.Fatalf("PUT deployment failed: %d", status)
	}
	if dep.Name != "gpt-4o-model" || dep.Properties.Model.Name != "gpt-4o" || dep.Sku.Capacity != 10 {
		t.Fatalf("unexpected deployment properties: %+v", dep)
	}

	// 2. GET Deployment
	var fetched CognitiveDeployment
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(depBase), nil, &fetched)
	if status != http.StatusOK || fetched.Properties.Model.Name != "gpt-4o" {
		t.Fatalf("GET deployment failed: %d", status)
	}

	// 3. LIST Deployments
	var listResp struct {
		Value []CognitiveDeployment `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(accBase+"/deployments"), nil, &listResp)
	if status != http.StatusOK || len(listResp.Value) != 1 {
		t.Fatalf("LIST deployments failed: status=%d len=%d", status, len(listResp.Value))
	}

	// 4. DELETE Deployment
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(depBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE deployment failed: %d", status)
	}

	// 5. GET after DELETE returns 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(depBase), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE expected 404, got %d", status)
	}
}

func TestDeploymentRequiresAccount(t *testing.T) {
	srv, _ := newTestServer(t)
	depBase := srv.URL + "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.CognitiveServices/accounts/non-existent/deployments/dep1"

	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(depBase), map[string]any{}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when deploying to missing account, got %d", status)
	}
}
