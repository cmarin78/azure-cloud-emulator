package network

import (
	"net/http"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

const (
	peSub = "00000000-0000-0000-0000-000000000000"
	peRG  = "test-rg"
)

func TestPrivateEndpointCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
		"/providers/Microsoft.Network/privateEndpoints"

	body := map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"subnet": map[string]any{
				"id": "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
					"/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default",
			},
			"privateLinkServiceConnections": []map[string]any{
				{
					"name": "myconn",
					"properties": map[string]any{
						"privateLinkServiceId": "/subscriptions/" + peSub +
							"/resourceGroups/" + peRG +
							"/providers/Microsoft.Storage/storageAccounts/mysa",
						"groupIds": []string{"blob"},
					},
				},
			},
		},
	}

	// PUT → 201
	var pe map[string]any
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/mype"), body, &pe)
	if status != http.StatusCreated {
		t.Fatalf("PUT PE: want 201, got %d", status)
	}
	if pe["name"] != "mype" {
		t.Fatalf("expected name mype, got %v", pe["name"])
	}
	props := pe["properties"].(map[string]any)
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("provisioningState: %v", props["provisioningState"])
	}
	conns := props["privateLinkServiceConnections"].([]any)
	if len(conns) == 0 {
		t.Fatal("expected at least one connection")
	}
	connProps := conns[0].(map[string]any)["properties"].(map[string]any)
	state := connProps["privateLinkServiceConnectionState"].(map[string]any)
	if state["status"] != "Approved" {
		t.Fatalf("expected Approved, got %v", state["status"])
	}
	nics := props["networkInterfaces"].([]any)
	if len(nics) == 0 {
		t.Fatal("expected injected networkInterfaces")
	}

	// PUT again → 200
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/mype"), body, nil)
	if status != http.StatusOK {
		t.Fatalf("PUT PE update: want 200, got %d", status)
	}

	// GET
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/mype"), nil, &pe)
	if status != http.StatusOK || pe["name"] != "mype" {
		t.Fatalf("GET PE: status=%d name=%v", status, pe["name"])
	}

	// LIST
	var list map[string]any
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("LIST PE: want 200, got %d", status)
	}
	values := list["value"].([]any)
	if len(values) != 1 {
		t.Fatalf("expected 1 PE in list, got %d", len(values))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base+"/mype"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE PE: want 204, got %d", status)
	}

	// GET after DELETE → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/mype"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", status)
	}
}

func TestPrivateDNSZoneGroupCRUD(t *testing.T) {
	srv := newTestServer(t)
	peBase := srv.URL + "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
		"/providers/Microsoft.Network/privateEndpoints"

	// Create parent PE
	peBody := map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"privateLinkServiceConnections": []any{},
		},
	}
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(peBase+"/myendpoint"), peBody, nil)
	if status != http.StatusCreated {
		t.Fatalf("PUT parent PE: want 201, got %d", status)
	}

	groupBase := peBase + "/myendpoint/privateDnsZoneGroups"
	groupBody := map[string]any{
		"properties": map[string]any{
			"privateDnsZoneConfigs": []map[string]any{
				{
					"name": "config1",
					"properties": map[string]any{
						"privateDnsZoneId": "/subscriptions/" + peSub +
							"/resourceGroups/" + peRG +
							"/providers/Microsoft.Network/privateDnsZones/privatelink.blob.core.windows.net",
					},
				},
			},
		},
	}

	var grp map[string]any
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(groupBase+"/mygroup"), groupBody, &grp)
	if status != http.StatusCreated {
		t.Fatalf("PUT DNS zone group: want 201, got %d", status)
	}
	if grp["name"] != "mygroup" {
		t.Fatalf("expected name mygroup, got %v", grp["name"])
	}
	grpProps := grp["properties"].(map[string]any)
	if grpProps["provisioningState"] != "Succeeded" {
		t.Fatalf("provisioningState: %v", grpProps["provisioningState"])
	}

	// GET
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(groupBase+"/mygroup"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET DNS zone group: want 200, got %d", status)
	}

	// LIST
	var list map[string]any
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(groupBase), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("LIST DNS zone groups: want 200, got %d", status)
	}
	values := list["value"].([]any)
	if len(values) != 1 {
		t.Fatalf("expected 1 group, got %d", len(values))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(groupBase+"/mygroup"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE DNS zone group: want 204, got %d", status)
	}

	// GET after DELETE → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(groupBase+"/mygroup"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", status)
	}
}

func TestPrivateLinkServiceCRUD(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
		"/providers/Microsoft.Network/privateLinkServices"

	body := map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"loadBalancerFrontendIpConfigurations": []map[string]any{
				{"id": "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
					"/providers/Microsoft.Network/loadBalancers/mylb/frontendIPConfigurations/myfrontend"},
			},
			"ipConfigurations": []map[string]any{
				{
					"name": "ipconfig1",
					"properties": map[string]any{
						"privateIpAllocationMethod": "Dynamic",
						"subnet": map[string]any{
							"id": "/subscriptions/" + peSub + "/resourceGroups/" + peRG +
								"/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default",
						},
						"primary": true,
					},
				},
			},
		},
	}

	// PUT → 201
	var pls map[string]any
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/mypls"), body, &pls)
	if status != http.StatusCreated {
		t.Fatalf("PUT PLS: want 201, got %d", status)
	}
	if pls["name"] != "mypls" {
		t.Fatalf("name mismatch: %v", pls["name"])
	}
	plsProps := pls["properties"].(map[string]any)
	if plsProps["provisioningState"] != "Succeeded" {
		t.Fatalf("provisioningState: %v", plsProps["provisioningState"])
	}
	alias, _ := plsProps["alias"].(string)
	if alias == "" {
		t.Fatal("expected non-empty alias")
	}

	// PUT again → 200
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base+"/mypls"), body, nil)
	if status != http.StatusOK {
		t.Fatalf("PUT PLS update: want 200, got %d", status)
	}

	// GET
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/mypls"), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET PLS: want 200, got %d", status)
	}

	// LIST
	var list map[string]any
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &list)
	if status != http.StatusOK {
		t.Fatalf("LIST PLS: want 200, got %d", status)
	}
	values := list["value"].([]any)
	if len(values) != 1 {
		t.Fatalf("expected 1 PLS, got %d", len(values))
	}

	// DELETE → 204
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base+"/mypls"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE PLS: want 204, got %d", status)
	}

	// GET after DELETE → 404
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base+"/mypls"), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", status)
	}
}
