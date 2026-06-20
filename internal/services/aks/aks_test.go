package aks

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	ops := server.NewOperations()
	mux := http.NewServeMux()
	New(db, ops).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestManagedClusterLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/mycluster"

	var cluster ManagedCluster
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location": "eastus",
		"identity": map[string]any{"type": "SystemAssigned"},
		"properties": map[string]any{
			"dnsPrefix": "mycluster",
		},
	}, &cluster)
	if status != http.StatusAccepted {
		t.Fatalf("put cluster: status=%d", status)
	}
	if cluster.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put cluster response: %+v", cluster)
	}
	if cluster.Properties.Fqdn == "" {
		t.Fatalf("expected a non-empty fqdn, got %+v", cluster.Properties)
	}
	if len(cluster.Properties.AgentPoolProfiles) != 1 || cluster.Properties.AgentPoolProfiles[0].Mode != "System" {
		t.Fatalf("expected a default System agent pool profile, got %+v", cluster.Properties.AgentPoolProfiles)
	}
	if cluster.Identity == nil || cluster.Identity.PrincipalID == "" {
		t.Fatalf("expected a fake principalId for SystemAssigned identity, got %+v", cluster.Identity)
	}

	var got ManagedCluster
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != http.StatusOK || got.Name != "mycluster" {
		t.Fatalf("get cluster: status=%d cluster=%+v", status, got)
	}

	var list struct {
		Value []ManagedCluster `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != http.StatusOK || len(list.Value) != 1 {
		t.Fatalf("list clusters: status=%d value=%+v", status, list.Value)
	}

	// listClusterUserCredential debe devolver un kubeconfig base64.
	var creds struct {
		Kubeconfigs []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"kubeconfigs"`
	}
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/listClusterUserCredential"), nil, &creds)
	if status != http.StatusOK || len(creds.Kubeconfigs) != 1 || creds.Kubeconfigs[0].Value == "" {
		t.Fatalf("listClusterUserCredential: status=%d creds=%+v", status, creds)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete cluster: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete cluster: want 204, got %d", status)
	}
}

func TestManagedClusterRequiresDNSPrefix(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/mycluster"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for missing dnsPrefix, got %d", status)
	}
}

func TestAgentPoolLifecycle(t *testing.T) {
	srv := newTestServer(t)
	clusterBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/mycluster"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(clusterBase), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"dnsPrefix": "mycluster"},
	}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("put cluster: status=%d", status)
	}

	poolBase := clusterBase + "/agentPools/userpool"
	var pool AgentPool
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(poolBase), map[string]any{
		"properties": map[string]any{
			"vmSize": "Standard_DS2_v2",
			"count":  3,
			"mode":   "User",
		},
	}, &pool)
	if status != http.StatusAccepted || pool.Properties.Count != 3 || pool.Properties.Mode != "User" {
		t.Fatalf("put agent pool: status=%d pool=%+v", status, pool)
	}

	var got AgentPool
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(poolBase), nil, &got)
	if status != http.StatusOK || got.Name != "userpool" {
		t.Fatalf("get agent pool: status=%d pool=%+v", status, got)
	}

	var list struct {
		Value []AgentPool `json:"value"`
	}
	listURL := clusterBase + "/agentPools"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	// El pool "default" sintetizado en el PUT del cluster, más "userpool".
	if status != http.StatusOK || len(list.Value) != 2 {
		t.Fatalf("list agent pools: status=%d value=%+v", status, list.Value)
	}

	// El cluster padre debe reflejar ambos pools en agentPoolProfiles.
	var cluster ManagedCluster
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(clusterBase), nil, &cluster)
	if len(cluster.Properties.AgentPoolProfiles) != 2 {
		t.Fatalf("expected 2 agent pool profiles on cluster, got %+v", cluster.Properties.AgentPoolProfiles)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(poolBase), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete agent pool: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(poolBase), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
}

func TestAgentPoolRejectsInvalidMode(t *testing.T) {
	srv := newTestServer(t)
	clusterBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/mycluster"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(clusterBase), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"dnsPrefix": "mycluster"},
	}, nil)

	poolBase := clusterBase + "/agentPools/badpool"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(poolBase), map[string]any{
		"properties": map[string]any{
			"vmSize": "Standard_DS2_v2",
			"mode":   "Bogus",
		},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid mode, got %d", status)
	}
}

func TestAgentPoolRequiresExistingCluster(t *testing.T) {
	srv := newTestServer(t)
	poolBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/missing/agentPools/pool1"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(poolBase), map[string]any{
		"properties": map[string]any{"vmSize": "Standard_DS2_v2"},
	}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 for missing parent cluster, got %d", status)
	}
}
