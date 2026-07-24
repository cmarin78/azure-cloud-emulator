package authorization

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

// TestPolicyDefinitionCRUD verifica el ciclo completo de policyDefinitions:
// PUT (201) → GET (verificar campos) → idempotent PUT (200) → LIST (1 item) →
// DELETE (204) → GET (404) → DELETE idempotent (204).
func TestPolicyDefinitionCRUD(t *testing.T) {
	srv := newTestServer(t)

	policyRule := json.RawMessage(`{"if":{"field":"type","equals":"Microsoft.Storage/storageAccounts"},"then":{"effect":"deny"}}`)
	params := json.RawMessage(`{"effect":{"type":"String","defaultValue":"deny"}}`)

	base := srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyDefinitions/deny-storage-accounts"
	url := testutil.WithAPIVersion(base)

	// PUT (201)
	var d PolicyDefinition
	status := testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"displayName": "Deny Storage Accounts",
			"description": "Denies creation of storage accounts",
			"policyType":  "Custom",
			"mode":        "All",
			"policyRule":  json.RawMessage(`{"if":{"field":"type","equals":"Microsoft.Storage/storageAccounts"},"then":{"effect":"deny"}}`),
			"parameters":  json.RawMessage(`{"effect":{"type":"String","defaultValue":"deny"}}`),
		},
	}, &d)
	if status != http.StatusCreated {
		t.Fatalf("put esperado 201, obtuvo %d", status)
	}
	if d.Name != "deny-storage-accounts" {
		t.Errorf("name esperado 'deny-storage-accounts', obtuvo %q", d.Name)
	}
	if d.Properties.PolicyType != "Custom" {
		t.Errorf("policyType esperado 'Custom', obtuvo %q", d.Properties.PolicyType)
	}
	if d.Properties.Mode != "All" {
		t.Errorf("mode esperado 'All', obtuvo %q", d.Properties.Mode)
	}
	if d.Properties.DisplayName != "Deny Storage Accounts" {
		t.Errorf("displayName incorrecto: %q", d.Properties.DisplayName)
	}
	_ = policyRule
	_ = params

	// GET
	var d2 PolicyDefinition
	status = testutil.DoJSON(t, "GET", url, nil, &d2)
	if status != http.StatusOK {
		t.Fatalf("get esperado 200, obtuvo %d", status)
	}
	if d2.Properties.PolicyType != "Custom" {
		t.Errorf("policyType en GET: %q", d2.Properties.PolicyType)
	}
	if len(d2.Properties.PolicyRule) == 0 {
		t.Error("policyRule vacío en GET")
	}
	if len(d2.Properties.Parameters) == 0 {
		t.Error("parameters vacío en GET")
	}

	// PUT idempotente (200)
	var d3 PolicyDefinition
	status = testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"displayName": "Deny Storage Accounts Updated",
			"policyType":  "Custom",
			"mode":        "All",
			"policyRule":  json.RawMessage(`{"if":{"field":"type","equals":"Microsoft.Storage/storageAccounts"},"then":{"effect":"deny"}}`),
		},
	}, &d3)
	if status != http.StatusOK {
		t.Fatalf("put idempotente esperado 200, obtuvo %d", status)
	}
	if d3.Properties.DisplayName != "Deny Storage Accounts Updated" {
		t.Errorf("displayName esperado updated: %q", d3.Properties.DisplayName)
	}

	// LIST
	listURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyDefinitions")
	var listResp struct {
		Value []PolicyDefinition `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", listURL, nil, &listResp)
	if status != http.StatusOK {
		t.Fatalf("list esperado 200, obtuvo %d", status)
	}
	if len(listResp.Value) != 1 {
		t.Fatalf("list esperado 1 item, obtuvo %d", len(listResp.Value))
	}

	// DELETE (204)
	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete esperado 204, obtuvo %d", status)
	}

	// GET (404)
	status = testutil.DoJSON(t, "GET", url, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get post-delete esperado 404, obtuvo %d", status)
	}

	// DELETE idempotente (204)
	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete idempotente esperado 204, obtuvo %d", status)
	}
}

// TestPolicyDefinitionDefaultValues verifica que policyType y mode tengan
// valores por defecto cuando el cliente no los manda.
func TestPolicyDefinitionDefaultValues(t *testing.T) {
	srv := newTestServer(t)
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyDefinitions/minimal-policy")

	var d PolicyDefinition
	status := testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"displayName": "Minimal Policy",
			"policyRule":  json.RawMessage(`{"if":{"field":"type","equals":"foo"},"then":{"effect":"audit"}}`),
		},
	}, &d)
	if status != http.StatusCreated {
		t.Fatalf("put esperado 201, obtuvo %d", status)
	}
	if d.Properties.PolicyType != "Custom" {
		t.Errorf("policyType default esperado 'Custom', obtuvo %q", d.Properties.PolicyType)
	}
	if d.Properties.Mode != "All" {
		t.Errorf("mode default esperado 'All', obtuvo %q", d.Properties.Mode)
	}
}

// TestPolicyAssignmentCRUD verifica el ciclo completo de policyAssignments
// a nivel de suscripción y de resource group.
func TestPolicyAssignmentCRUD(t *testing.T) {
	srv := newTestServer(t)

	policyDefID := "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyDefinitions/deny-storage-accounts"

	// --- scope de suscripción ---
	subBase := srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyAssignments/deny-storage-assign"
	subURL := testutil.WithAPIVersion(subBase)

	var a PolicyAssignment
	status := testutil.DoJSON(t, "PUT", subURL, map[string]any{
		"properties": map[string]any{
			"policyDefinitionId": policyDefID,
			"displayName":        "Deny storage accounts",
			"enforcementMode":    "Default",
		},
	}, &a)
	if status != http.StatusCreated {
		t.Fatalf("put sub-scope esperado 201, obtuvo %d", status)
	}
	if a.Name != "deny-storage-assign" {
		t.Errorf("name esperado 'deny-storage-assign', obtuvo %q", a.Name)
	}
	if a.Properties.EnforcementMode != "Default" {
		t.Errorf("enforcementMode esperado 'Default', obtuvo %q", a.Properties.EnforcementMode)
	}
	expectedScope := "/subscriptions/" + subID
	if a.Properties.Scope != expectedScope {
		t.Errorf("scope esperado %q, obtuvo %q", expectedScope, a.Properties.Scope)
	}

	// GET suscripción
	var a2 PolicyAssignment
	status = testutil.DoJSON(t, "GET", subURL, nil, &a2)
	if status != http.StatusOK {
		t.Fatalf("get sub-scope esperado 200, obtuvo %d", status)
	}
	if a2.Properties.PolicyDefinitionID != policyDefID {
		t.Errorf("policyDefinitionId incorrecto: %q", a2.Properties.PolicyDefinitionID)
	}

	// LIST suscripción
	listSubURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/policyAssignments")
	var listSub struct {
		Value []PolicyAssignment `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", listSubURL, nil, &listSub)
	if status != http.StatusOK {
		t.Fatalf("list sub-scope esperado 200, obtuvo %d", status)
	}
	if len(listSub.Value) != 1 {
		t.Fatalf("list sub-scope esperado 1, obtuvo %d", len(listSub.Value))
	}

	// --- scope de resource group ---
	rgBase := srv.URL + "/subscriptions/" + subID + "/resourceGroups/test-rg/providers/Microsoft.Authorization/policyAssignments/deny-storage-rg"
	rgURL := testutil.WithAPIVersion(rgBase)

	var b PolicyAssignment
	status = testutil.DoJSON(t, "PUT", rgURL, map[string]any{
		"properties": map[string]any{
			"policyDefinitionId": policyDefID,
			"displayName":        "Deny storage accounts RG scope",
		},
	}, &b)
	if status != http.StatusCreated {
		t.Fatalf("put rg-scope esperado 201, obtuvo %d", status)
	}
	expectedRGScope := "/subscriptions/" + subID + "/resourceGroups/test-rg"
	if b.Properties.Scope != expectedRGScope {
		t.Errorf("scope rg esperado %q, obtuvo %q", expectedRGScope, b.Properties.Scope)
	}
	if b.Properties.EnforcementMode != "Default" {
		t.Errorf("enforcementMode default esperado 'Default', obtuvo %q", b.Properties.EnforcementMode)
	}

	// LIST resource group
	listRGURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/resourceGroups/test-rg/providers/Microsoft.Authorization/policyAssignments")
	var listRG struct {
		Value []PolicyAssignment `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", listRGURL, nil, &listRG)
	if status != http.StatusOK {
		t.Fatalf("list rg-scope esperado 200, obtuvo %d", status)
	}
	if len(listRG.Value) != 1 {
		t.Fatalf("list rg-scope esperado 1, obtuvo %d", len(listRG.Value))
	}

	// DELETE sub-scope
	status = testutil.DoJSON(t, "DELETE", subURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete sub-scope esperado 204, obtuvo %d", status)
	}
	status = testutil.DoJSON(t, "GET", subURL, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get post-delete sub-scope esperado 404, obtuvo %d", status)
	}

	// DELETE rg-scope
	status = testutil.DoJSON(t, "DELETE", rgURL, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete rg-scope esperado 204, obtuvo %d", status)
	}
}
