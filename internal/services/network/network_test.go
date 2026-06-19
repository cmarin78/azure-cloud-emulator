package network

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

func TestVirtualNetworkAndSubnetLifecycle(t *testing.T) {
	srv := newTestServer(t)
	vnetBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"

	var vnet VirtualNetwork
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase), virtualNetworkRequest{
		Location: "eastus",
		Properties: struct {
			AddressSpace AddressSpace `json:"addressSpace"`
		}{AddressSpace: AddressSpace{AddressPrefixes: []string{"10.0.0.0/16"}}},
	}, &vnet)
	if status != http.StatusCreated || vnet.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put vnet: status=%d vnet=%+v", status, vnet)
	}

	// Update (existing) -> 200.
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase), virtualNetworkRequest{
		Location: "eastus",
		Properties: struct {
			AddressSpace AddressSpace `json:"addressSpace"`
		}{AddressSpace: AddressSpace{AddressPrefixes: []string{"10.0.0.0/16"}}},
	}, &vnet)
	if status != http.StatusOK {
		t.Fatalf("update vnet: want 200, got %d", status)
	}

	subnetBase := vnetBase + "/subnets/subnet1"
	var subnet Subnet
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(subnetBase), subnetRequest{
		Properties: struct {
			AddressPrefix        string     `json:"addressPrefix"`
			NetworkSecurityGroup *Reference `json:"networkSecurityGroup,omitempty"`
			RouteTable           *Reference `json:"routeTable,omitempty"`
		}{AddressPrefix: "10.0.1.0/24"},
	}, &subnet)
	if status != http.StatusCreated || subnet.Properties.AddressPrefix != "10.0.1.0/24" {
		t.Fatalf("put subnet: status=%d subnet=%+v", status, subnet)
	}

	var got Subnet
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(subnetBase), nil, &got)
	if status != 200 || got.Name != "subnet1" {
		t.Fatalf("get subnet: status=%d subnet=%+v", status, got)
	}

	var list struct {
		Value []Subnet `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(vnetBase+"/subnets"), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list subnets: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(subnetBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete subnet: status=%d", status)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(vnetBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete vnet: status=%d", status)
	}

	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(vnetBase), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}

	// Idempotent delete.
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(vnetBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete: want 204, got %d", status)
	}
}

func TestVirtualNetworkRequiresLocationAndAddressSpace(t *testing.T) {
	srv := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1")

	status := testutil.DoJSON(t, "PUT", base, map[string]any{}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing location: want 400, got %d", status)
	}

	status = testutil.DoJSON(t, "PUT", base, map[string]any{"location": "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("missing addressSpace: want 400, got %d", status)
	}
}

func TestNetworkInterfaceLifecycle(t *testing.T) {
	srv := newTestServer(t)
	vnetBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase), virtualNetworkRequest{
		Location: "eastus",
		Properties: struct {
			AddressSpace AddressSpace `json:"addressSpace"`
		}{AddressSpace: AddressSpace{AddressPrefixes: []string{"10.0.0.0/16"}}},
	}, nil)
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase+"/subnets/subnet1"), subnetRequest{
		Properties: struct {
			AddressPrefix        string     `json:"addressPrefix"`
			NetworkSecurityGroup *Reference `json:"networkSecurityGroup,omitempty"`
			RouteTable           *Reference `json:"routeTable,omitempty"`
		}{AddressPrefix: "10.0.1.0/24"},
	}, nil)

	subnetID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/subnet1"

	nicBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	reqBody := map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"ipConfigurations": []map[string]any{
				{
					"name": "ipconfig1",
					"properties": map[string]any{
						"subnet": map[string]any{"id": subnetID},
					},
				},
			},
		},
	}

	var nic NetworkInterface
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nicBase), reqBody, &nic)
	if status != http.StatusCreated {
		t.Fatalf("put nic: status=%d", status)
	}
	if len(nic.Properties.IPConfigurations) != 1 || nic.Properties.IPConfigurations[0].Properties.PrivateIPAddress == "" {
		t.Fatalf("nic missing allocated IP: %+v", nic)
	}
	if nic.Properties.IPConfigurations[0].Properties.PrivateIPAllocationMethod != "Dynamic" {
		t.Fatalf("expected default Dynamic allocation, got %+v", nic.Properties.IPConfigurations[0].Properties)
	}

	var got NetworkInterface
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(nicBase), nil, &got)
	if status != 200 || got.Name != "nic1" {
		t.Fatalf("get nic: status=%d nic=%+v", status, got)
	}

	var list struct {
		Value []NetworkInterface `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list nics: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(nicBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete nic: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(nicBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete nic: want 204, got %d", status)
	}
}

func TestNetworkInterfaceRejectsInvalidSubnetReference(t *testing.T) {
	srv := newTestServer(t)
	nicBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	reqBody := map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"ipConfigurations": []map[string]any{
				{
					"properties": map[string]any{
						"subnet": map[string]any{"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/missing/subnets/missing"},
					},
				},
			},
		},
	}
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nicBase), reqBody, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid subnet reference, got %d", status)
	}
}
