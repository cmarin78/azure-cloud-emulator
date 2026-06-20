package authorization

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	mux := http.NewServeMux()
	New(db).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const subID = "00000000-0000-0000-0000-000000000001"

func TestRoleDefinitionLifecycle(t *testing.T) {
	srv := newTestServer(t)
	roleDefID := "11111111-1111-1111-1111-111111111111"
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions/" + roleDefID)

	var rd RoleDefinition
	status := testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"roleName":         "phase15-custom-role",
			"description":      "rol de prueba",
			"assignableScopes": []string{"/subscriptions/" + subID},
			"permissions": []map[string]any{
				{"actions": []string{"Microsoft.Storage/storageAccounts/read"}},
			},
		},
	}, &rd)
	if status != http.StatusCreated || rd.Properties.RoleName != "phase15-custom-role" || rd.Properties.RoleType != "CustomRole" {
		t.Fatalf("put role definition: status=%d rd=%+v", status, rd)
	}

	// PUT de nuevo sobre el mismo id -> actualización (200, no 201).
	status = testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"roleName":         "phase15-custom-role-v2",
			"assignableScopes": []string{"/subscriptions/" + subID},
		},
	}, &rd)
	if status != http.StatusOK || rd.Properties.RoleName != "phase15-custom-role-v2" {
		t.Fatalf("update role definition: status=%d rd=%+v", status, rd)
	}

	var got RoleDefinition
	status = testutil.DoJSON(t, "GET", url, nil, &got)
	if status != http.StatusOK || got.Name != roleDefID {
		t.Fatalf("get role definition: status=%d got=%+v", status, got)
	}

	var list struct {
		Value []RoleDefinition `json:"value"`
	}
	listURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions")
	status = testutil.DoJSON(t, "GET", listURL, nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list role definitions: status=%d list=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete role definition: status=%d", status)
	}
	// idempotente
	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete role definition (again): status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", url, nil, &got)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
}

func TestRoleDefinitionRequiresRoleNameAndAssignableScopes(t *testing.T) {
	srv := newTestServer(t)
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions/22222222-2222-2222-2222-222222222222")

	status := testutil.DoJSON(t, "PUT", url, map[string]any{"properties": map[string]any{}}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without roleName, got %d", status)
	}

	status = testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{"roleName": "x"},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without assignableScopes, got %d", status)
	}
}

func TestRoleAssignmentSubscriptionScope(t *testing.T) {
	srv := newTestServer(t)
	name := "33333333-3333-3333-3333-333333333333"
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleAssignments/" + name)

	var ra RoleAssignment
	status := testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"roleDefinitionId": "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions/reader",
			"principalId":      "some-object-id",
		},
	}, &ra)
	if status != http.StatusCreated || ra.Properties.Scope != "/subscriptions/"+subID || ra.Properties.PrincipalID != "some-object-id" {
		t.Fatalf("put role assignment: status=%d ra=%+v", status, ra)
	}

	var got RoleAssignment
	status = testutil.DoJSON(t, "GET", url, nil, &got)
	if status != http.StatusOK || got.Name != name {
		t.Fatalf("get role assignment: status=%d got=%+v", status, got)
	}

	var list struct {
		Value []RoleAssignment `json:"value"`
	}
	listURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleAssignments")
	status = testutil.DoJSON(t, "GET", listURL, nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list role assignments (sub scope): status=%d list=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete role assignment: status=%d", status)
	}
}

func TestRoleAssignmentResourceGroupScope(t *testing.T) {
	srv := newTestServer(t)
	rg := "demo-rg"
	name := "44444444-4444-4444-4444-444444444444"
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/resourceGroups/" + rg + "/providers/Microsoft.Authorization/roleAssignments/" + name)

	var ra RoleAssignment
	status := testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{
			"roleDefinitionId": "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions/contributor",
			"principalId":      "another-object-id",
		},
	}, &ra)
	wantScope := "/subscriptions/" + subID + "/resourceGroups/" + rg
	if status != http.StatusCreated || ra.Properties.Scope != wantScope {
		t.Fatalf("put role assignment (rg scope): status=%d ra=%+v", status, ra)
	}

	// La lista a nivel de suscripción NO debe incluir esta asignación de
	// resource group -- exactamente el bug de colisión de prefijo que el
	// diseño de dos buckets separados evita.
	var subList struct {
		Value []RoleAssignment `json:"value"`
	}
	subListURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleAssignments")
	status = testutil.DoJSON(t, "GET", subListURL, nil, &subList)
	if status != http.StatusOK || len(subList.Value) != 0 {
		t.Fatalf("expected subscription-scope list to stay empty, got status=%d list=%+v", status, subList.Value)
	}

	var rgList struct {
		Value []RoleAssignment `json:"value"`
	}
	rgListURL := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/resourceGroups/" + rg + "/providers/Microsoft.Authorization/roleAssignments")
	status = testutil.DoJSON(t, "GET", rgListURL, nil, &rgList)
	if status != http.StatusOK || len(rgList.Value) != 1 {
		t.Fatalf("list role assignments (rg scope): status=%d list=%+v", status, rgList.Value)
	}

	status = testutil.DoJSON(t, "DELETE", url, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete role assignment (rg scope): status=%d", status)
	}
}

func TestRoleAssignmentRequiresRoleDefinitionIDAndPrincipalID(t *testing.T) {
	srv := newTestServer(t)
	url := testutil.WithAPIVersion(srv.URL + "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleAssignments/55555555-5555-5555-5555-555555555555")

	status := testutil.DoJSON(t, "PUT", url, map[string]any{"properties": map[string]any{}}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without roleDefinitionId, got %d", status)
	}

	status = testutil.DoJSON(t, "PUT", url, map[string]any{
		"properties": map[string]any{"roleDefinitionId": "x"},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 without principalId, got %d", status)
	}
}
