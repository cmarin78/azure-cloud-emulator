package deployments

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/storageaccounts"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

// newTestServer registra deployments junto con storageaccounts en el
// mismo *http.ServeMux: el dispatcher de deployments necesita un
// servicio real al que reenviar sus PUT sintéticos, y storageaccounts es
// el más simple de los servicios ARM existentes para ese propósito.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	ops := server.NewOperations()
	mux := http.NewServeMux()
	storageaccounts.New(db, ops).Register(mux)
	New(db, ops, mux).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func deploymentURL(srv *httptest.Server, name string) string {
	return srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Resources/deployments/" + name
}

func TestDeploymentDispatchesResourceAndPersistsOperations(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "dep1")

	body := map[string]any{
		"properties": map[string]any{
			"mode": "Incremental",
			"template": map[string]any{
				"parameters": map[string]any{
					"storageName": map[string]any{"type": "string"},
				},
				"variables": map[string]any{
					"skuName": "Standard_LRS",
				},
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "[parameters('storageName')]",
						"location":   "eastus",
						"sku":        map[string]any{"name": "[variables('skuName')]"},
					},
				},
			},
			"parameters": map[string]any{
				"storageName": map[string]any{"value": "deptest1"},
			},
		},
	}

	var dep Deployment
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &dep)
	if status != http.StatusCreated {
		t.Fatalf("put deployment: status=%d dep=%+v", status, dep)
	}
	if dep.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("expected Succeeded, got %+v", dep.Properties)
	}
	if len(dep.Properties.OutputResources) != 1 {
		t.Fatalf("expected 1 output resource, got %+v", dep.Properties.OutputResources)
	}
	wantPath := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/deptest1"
	if dep.Properties.OutputResources[0].ID != wantPath {
		t.Fatalf("output resource id = %q, want %q", dep.Properties.OutputResources[0].ID, wantPath)
	}

	// El recurso debe haber sido realmente creado por el dispatcher.
	acctURL := srv.URL + wantPath
	var acct storageaccounts.StorageAccount
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(acctURL), nil, &acct)
	if status != http.StatusOK || acct.Name != "deptest1" {
		t.Fatalf("expected dispatched storage account to exist, status=%d acct=%+v", status, acct)
	}

	// GET del deployment debe reflejar lo mismo que devolvió el PUT.
	var got Deployment
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("get deployment: status=%d dep=%+v", status, got)
	}

	// Las operations deben tener una entrada Succeeded por el único
	// recurso despachado.
	var ops struct {
		Value []DeploymentOperation `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/operations"), nil, &ops)
	if status != http.StatusOK || len(ops.Value) != 1 {
		t.Fatalf("list operations: status=%d ops=%+v", status, ops.Value)
	}
	if ops.Value[0].Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("expected operation Succeeded, got %+v", ops.Value[0].Properties)
	}
	if ops.Value[0].Properties.TargetResource.ResourceName != "deptest1" {
		t.Fatalf("unexpected target resource: %+v", ops.Value[0].Properties.TargetResource)
	}
}

func TestDeploymentOrdersByDependsOn(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "dep2")

	// Dos storage accounts; el segundo declara dependsOn sobre el primero
	// vía resourceId(). Como ambos son del mismo tipo de recurso (no hay
	// forma de observar el orden de creación directamente desde fuera),
	// lo que valida este test es que el dependsOn se resuelve sin error y
	// que la operation list refleja el orden topológico (el dependiente
	// después del que depende).
	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "depbase",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
					},
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "depdependent",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
						"dependsOn": []any{
							"[resourceId('Microsoft.Storage/storageAccounts', 'depbase')]",
						},
					},
				},
			},
		},
	}

	var dep Deployment
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &dep)
	if status != http.StatusCreated || dep.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put deployment: status=%d dep=%+v", status, dep)
	}

	var ops struct {
		Value []DeploymentOperation `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/operations"), nil, &ops)
	if status != http.StatusOK || len(ops.Value) != 2 {
		t.Fatalf("list operations: status=%d ops=%+v", status, ops.Value)
	}
	if ops.Value[0].Properties.TargetResource.ResourceName != "depbase" ||
		ops.Value[1].Properties.TargetResource.ResourceName != "depdependent" {
		t.Fatalf("expected depbase before depdependent, got order: %s, %s",
			ops.Value[0].Properties.TargetResource.ResourceName,
			ops.Value[1].Properties.TargetResource.ResourceName)
	}
}

func TestDeploymentFailsWhenDispatchedResourceErrors(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "depfail")

	// storageAccounts exige sku.name; omitirlo hace que el PUT despachado
	// responda 400, y el deployment completo debe reflejar ese fallo.
	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "willfail",
						"location":   "eastus",
					},
				},
			},
		},
	}

	var dep Deployment
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &dep)
	if status != http.StatusCreated {
		t.Fatalf("put deployment: status=%d dep=%+v", status, dep)
	}
	if dep.Properties.ProvisioningState != "Failed" {
		t.Fatalf("expected Failed, got %+v", dep.Properties)
	}
	if dep.Properties.Error == nil || dep.Properties.Error.Code == "" {
		t.Fatalf("expected populated error, got %+v", dep.Properties.Error)
	}

	// El recurso no debe haber sido creado.
	acctURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/willfail"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(acctURL), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected failed resource to not exist, got status=%d", status)
	}
}

func TestDeploymentMissingRequiredParameter(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "depmissingparam")

	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"parameters": map[string]any{
					"storageName": map[string]any{"type": "string"},
				},
				"resources": []any{},
			},
		},
	}
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required parameter, got %d", status)
	}
}

func TestDeploymentDependencyCycleDetected(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "depcycle")

	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "cyclea",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
						"dependsOn": []any{
							"[resourceId('Microsoft.Storage/storageAccounts', 'cycleb')]",
						},
					},
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "cycleb",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
						"dependsOn": []any{
							"[resourceId('Microsoft.Storage/storageAccounts', 'cyclea')]",
						},
					},
				},
			},
		},
	}
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for dependency cycle, got %d", status)
	}
}

func TestValidateDoesNotCreateResources(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "depvalidate")

	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "shouldnotexist",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
					},
				},
			},
		},
	}

	var dep Deployment
	status := testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/validate"), body, &dep)
	if status != http.StatusOK || dep.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("validate: status=%d dep=%+v", status, dep)
	}
	if len(dep.Properties.OutputResources) != 1 {
		t.Fatalf("expected 1 output resource from validate, got %+v", dep.Properties.OutputResources)
	}

	acctURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/shouldnotexist"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(acctURL), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("validate must not create resources, but found status=%d", status)
	}
}

func TestDeploymentDeleteIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	base := deploymentURL(srv, "depdelete")

	body := map[string]any{
		"properties": map[string]any{
			"template": map[string]any{
				"resources": []any{
					map[string]any{
						"type":       "Microsoft.Storage/storageAccounts",
						"apiVersion": "2023-01-01",
						"name":       "deldepacct",
						"location":   "eastus",
						"sku":        map[string]any{"name": "Standard_LRS"},
					},
				},
			},
		},
	}
	var dep Deployment
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, &dep)
	if status != http.StatusCreated {
		t.Fatalf("put deployment: status=%d", status)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete deployment: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete: want 204, got %d", status)
	}

	// El recurso que el deployment creó NO debe haber sido borrado: borrar
	// un deployment solo borra su propio registro, no los recursos que
	// creó — igual que Azure real.
	acctURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/deldepacct"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(acctURL), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("expected dispatched resource to survive deployment delete, got status=%d", status)
	}
}
