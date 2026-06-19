package compute

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/network"
	"github.com/cesarmarin/azure-emulator/internal/storage"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

// newTestServer wires network (needed for NIC/subnet validation referenced
// by VM creation) alongside compute, mirroring how main.go wires both real
// services together.
func newTestServer(t *testing.T) (*httptest.Server, *storage.DB) {
	t.Helper()
	db := testutil.NewDB(t)
	ops := server.NewOperations()
	netSvc := network.New(db)
	mux := http.NewServeMux()
	netSvc.Register(mux)
	New(db, ops, netSvc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, db
}

func TestDiskLifecycle(t *testing.T) {
	srv, _ := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1"

	var disk Disk
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), diskRequest{
		Location: "eastus",
		Properties: struct {
			DiskSizeGB   int          `json:"diskSizeGB"`
			CreationData CreationData `json:"creationData"`
		}{DiskSizeGB: 64, CreationData: CreationData{CreateOption: "Empty"}},
	}, &disk)
	if status != http.StatusCreated || disk.Properties.DiskSizeGB != 64 || disk.Sku.Name != "Standard_LRS" {
		t.Fatalf("put disk: status=%d disk=%+v", status, disk)
	}

	var got Disk
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "disk1" {
		t.Fatalf("get disk: status=%d disk=%+v", status, got)
	}

	var list struct {
		Value []Disk `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list disks: status=%d value=%+v", status, list.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete disk: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete disk: want 204, got %d", status)
	}
}

func TestDiskRequiresSizeWhenEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	base := testutil.WithAPIVersion(srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1")
	status := testutil.DoJSON(t, "PUT", base, diskRequest{Location: "eastus"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 without diskSizeGB, got %d", status)
	}
}

func TestListImageVersions(t *testing.T) {
	srv, _ := newTestServer(t)
	url := srv.URL + "/subscriptions/sub1/providers/Microsoft.Compute/locations/eastus/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus/22_04-lts-gen2/versions"

	var list struct {
		Value []imageVersion `json:"value"`
	}
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(url), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list image versions: status=%d value=%+v", status, list.Value)
	}
}

func TestListImageVersionsUnknownCombination404(t *testing.T) {
	srv, _ := newTestServer(t)
	url := srv.URL + "/subscriptions/sub1/providers/Microsoft.Compute/locations/eastus/publishers/Unknown/artifacttypes/vmimage/offers/unknown/skus/unknown/versions"
	status := testutil.DoJSON(t, "GET", testutil.WithAPIVersion(url), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404, got %d", status)
	}
}

// setupNIC creates a vnet/subnet/NIC via the wired network service and
// returns the NIC's resource ID, for use in VM creation requests.
func setupNIC(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	vnetBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"addressSpace": map[string]any{"addressPrefixes": []string{"10.0.0.0/16"}}},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup vnet: status=%d", status)
	}
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase+"/subnets/subnet1"), map[string]any{
		"properties": map[string]any{"addressPrefix": "10.0.1.0/24"},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup subnet: status=%d", status)
	}
	subnetID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/subnet1"

	nicBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nicBase), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"ipConfigurations": []map[string]any{
				{"properties": map[string]any{"subnet": map[string]any{"id": subnetID}}},
			},
		},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("setup nic: status=%d", status)
	}
	return "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
}

func vmRequestBody(nicID string) map[string]any {
	return map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"hardwareProfile": map[string]any{"vmSize": "Standard_B1s"},
			"storageProfile": map[string]any{
				"imageReference": map[string]any{
					"publisher": "Canonical",
					"offer":     "0001-com-ubuntu-server-jammy",
					"sku":       "22_04-lts-gen2",
					"version":   "latest",
				},
			},
			"osProfile": map[string]any{
				"computerName":  "myvm",
				"adminUsername": "azureuser",
				"adminPassword": "doesnotmatter",
			},
			"networkProfile": map[string]any{
				"networkInterfaces": []map[string]any{{"id": nicID}},
			},
		},
	}
}

func TestVirtualMachineLifecycle(t *testing.T) {
	srv, _ := newTestServer(t)
	nicID := setupNIC(t, srv)

	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/myvm"
	var vm VirtualMachine
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), vmRequestBody(nicID), &vm)
	if status != http.StatusAccepted {
		t.Fatalf("put vm: status=%d", status)
	}
	if vm.Properties.ProvisioningState != "Succeeded" || vm.Properties.StorageProfile.ImageReference.Version != "22.04.202401010" {
		t.Fatalf("put vm response: %+v", vm)
	}

	var got VirtualMachine
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if status != 200 || got.Name != "myvm" {
		t.Fatalf("get vm: status=%d vm=%+v", status, got)
	}

	var list struct {
		Value []VirtualMachine `json:"value"`
	}
	listURL := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines"
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(listURL), nil, &list)
	if status != 200 || len(list.Value) != 1 {
		t.Fatalf("list vms: status=%d value=%+v", status, list.Value)
	}

	// Power off then start.
	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/powerOff"), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("power off: status=%d", status)
	}
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if got.Properties.InstanceView.Statuses[1].Code != "PowerState/stopped" {
		t.Fatalf("expected stopped after powerOff: %+v", got.Properties.InstanceView)
	}

	status = testutil.DoJSON(t, "POST", testutil.WithAPIVersion(base+"/start"), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("start: status=%d", status)
	}
	testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, &got)
	if got.Properties.InstanceView.Statuses[1].Code != "PowerState/running" {
		t.Fatalf("expected running after start: %+v", got.Properties.InstanceView)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete vm: status=%d", status)
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(base), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete vm: want 204, got %d", status)
	}
}

func TestVirtualMachineRejectsInvalidNICReference(t *testing.T) {
	srv, _ := newTestServer(t)
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/myvm"
	missingNICID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/missing"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), vmRequestBody(missingNICID), nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for missing NIC reference, got %d", status)
	}
}

func TestVirtualMachineRejectsUnknownImage(t *testing.T) {
	srv, _ := newTestServer(t)
	nicID := setupNIC(t, srv)
	body := vmRequestBody(nicID)
	body["properties"].(map[string]any)["storageProfile"] = map[string]any{
		"imageReference": map[string]any{
			"publisher": "Unknown",
			"offer":     "unknown",
			"sku":       "unknown",
		},
	}
	base := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/myvm"
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(base), body, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown image, got %d", status)
	}
}
