package network

import (
	"net/http"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func TestNetworkSecurityGroupAndRuleLifecycle(t *testing.T) {
	srv := newTestServer(t)
	nsgBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"

	var nsg NetworkSecurityGroup
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsgBase), networkSecurityGroupRequest{
		Location: "eastus",
	}, &nsg)
	if status != http.StatusCreated || nsg.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("put nsg: status=%d nsg=%+v", status, nsg)
	}

	ruleBase := nsgBase + "/securityRules/allow-ssh"
	var rule SecurityRule
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(ruleBase), map[string]any{
		"properties": map[string]any{
			"priority":                 100,
			"direction":                "Inbound",
			"access":                   "Allow",
			"protocol":                 "Tcp",
			"sourceAddressPrefix":      "*",
			"destinationAddressPrefix": "*",
			"sourcePortRange":          "*",
			"destinationPortRange":     "22",
		},
	}, &rule)
	if status != http.StatusCreated || rule.Properties.Priority != 100 {
		t.Fatalf("put security rule: status=%d rule=%+v", status, rule)
	}

	// Rechaza prioridades fuera de rango.
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsgBase+"/securityRules/bad"), map[string]any{
		"properties": map[string]any{
			"priority":  50,
			"direction": "Inbound",
			"access":    "Allow",
		},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d", status)
	}

	var got NetworkSecurityGroup
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(nsgBase), nil, &got)
	if status != http.StatusOK || len(got.Properties.SecurityRules) != 1 {
		t.Fatalf("get nsg: status=%d nsg=%+v", status, got)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(ruleBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete security rule: status=%d", status)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(nsgBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete nsg: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(nsgBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete nsg: want 204, got %d", status)
	}
}

func TestPublicIPAddressLifecycle(t *testing.T) {
	srv := newTestServer(t)
	pipBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/publicIPAddresses/pip1"

	var pip PublicIPAddress
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(pipBase), map[string]any{
		"location": "eastus",
	}, &pip)
	if status != http.StatusCreated {
		t.Fatalf("put public ip: status=%d", status)
	}
	if pip.Properties.PublicIPAllocationMethod != "Dynamic" || pip.Properties.IPAddress == "" {
		t.Fatalf("expected default allocation method + fake ip, got %+v", pip.Properties)
	}
	firstIP := pip.Properties.IPAddress

	// Un segundo PUT (update) debe conservar la misma IP determinista.
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(pipBase), map[string]any{
		"location": "eastus",
		"sku":      map[string]any{"name": "Standard"},
		"properties": map[string]any{
			"publicIPAllocationMethod": "Static",
		},
	}, &pip)
	if status != http.StatusOK || pip.Properties.IPAddress != firstIP || pip.SKU.Name != "Standard" {
		t.Fatalf("update public ip: status=%d pip=%+v", status, pip)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(pipBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete public ip: status=%d", status)
	}
}

func TestLoadBalancerLifecycle(t *testing.T) {
	srv := newTestServer(t)
	pipBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/publicIPAddresses/lbpip"
	var pip PublicIPAddress
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(pipBase), map[string]any{"location": "eastus"}, &pip)

	lbBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/loadBalancers/lb1"
	var lb LoadBalancer
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(lbBase), map[string]any{
		"location": "eastus",
		"properties": map[string]any{
			"frontendIPConfigurations": []map[string]any{
				{"name": "frontend1", "properties": map[string]any{"publicIPAddress": map[string]any{"id": pip.ID}}},
			},
			"backendAddressPools": []map[string]any{
				{"name": "backend1"},
			},
			"loadBalancingRules": []map[string]any{
				{
					"name": "rule1",
					"properties": map[string]any{
						"frontendIPConfiguration": map[string]any{"id": lbBase + "/frontendIPConfigurations/frontend1"},
						"backendAddressPool":      map[string]any{"id": lbBase + "/backendAddressPools/backend1"},
						"protocol":                "Tcp",
						"frontendPort":            80,
						"backendPort":             8080,
					},
				},
			},
		},
	}, &lb)
	if status != http.StatusCreated {
		t.Fatalf("put load balancer: status=%d", status)
	}
	if len(lb.Properties.FrontendIPConfigurations) != 1 || len(lb.Properties.BackendAddressPools) != 1 || len(lb.Properties.LoadBalancingRules) != 1 {
		t.Fatalf("unexpected lb shape: %+v", lb.Properties)
	}
	if lb.Properties.FrontendIPConfigurations[0].Properties.PublicIPAddress == nil ||
		lb.Properties.FrontendIPConfigurations[0].Properties.PublicIPAddress.ID != pip.ID {
		t.Fatalf("expected frontend to reference public ip by id, got %+v", lb.Properties.FrontendIPConfigurations[0])
	}

	var got LoadBalancer
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(lbBase), nil, &got)
	if status != http.StatusOK || got.Name != "lb1" {
		t.Fatalf("get load balancer: status=%d lb=%+v", status, got)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(lbBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete load balancer: status=%d", status)
	}
}

func TestRouteTableAndRouteLifecycle(t *testing.T) {
	srv := newTestServer(t)
	rtBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/routeTables/rt1"

	var rt RouteTable
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(rtBase), map[string]any{
		"location": "eastus",
	}, &rt)
	if status != http.StatusCreated {
		t.Fatalf("put route table: status=%d", status)
	}

	routeBase := rtBase + "/routes/to-internet"
	var route Route
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(routeBase), map[string]any{
		"properties": map[string]any{
			"addressPrefix": "0.0.0.0/0",
			"nextHopType":   "Internet",
		},
	}, &route)
	if status != http.StatusCreated || route.Properties.NextHopType != "Internet" {
		t.Fatalf("put route: status=%d route=%+v", status, route)
	}

	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(rtBase+"/routes/bad"), map[string]any{
		"properties": map[string]any{
			"addressPrefix": "0.0.0.0/0",
			"nextHopType":   "NotARealType",
		},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid nextHopType, got %d", status)
	}

	var got RouteTable
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(rtBase), nil, &got)
	if status != http.StatusOK || len(got.Properties.Routes) != 1 {
		t.Fatalf("get route table: status=%d rt=%+v", status, got)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(routeBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete route: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(rtBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete route table: status=%d", status)
	}
}

func TestSubnetCanReferenceNSGAndRouteTable(t *testing.T) {
	srv := newTestServer(t)
	nsgBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"
	var nsg NetworkSecurityGroup
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(nsgBase), map[string]any{"location": "eastus"}, &nsg)

	rtBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/routeTables/rt1"
	var rt RouteTable
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(rtBase), map[string]any{"location": "eastus"}, &rt)

	vnetBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"
	testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(vnetBase), map[string]any{
		"location":   "eastus",
		"properties": map[string]any{"addressSpace": map[string]any{"addressPrefixes": []string{"10.0.0.0/16"}}},
	}, nil)

	subnetBase := vnetBase + "/subnets/subnet1"
	var subnet Subnet
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(subnetBase), map[string]any{
		"properties": map[string]any{
			"addressPrefix":        "10.0.1.0/24",
			"networkSecurityGroup": map[string]any{"id": nsg.ID},
			"routeTable":           map[string]any{"id": rt.ID},
		},
	}, &subnet)
	if status != http.StatusCreated {
		t.Fatalf("put subnet with nsg+rt refs: status=%d", status)
	}
	if subnet.Properties.NetworkSecurityGroup == nil || subnet.Properties.NetworkSecurityGroup.ID != nsg.ID {
		t.Fatalf("expected subnet to reference nsg, got %+v", subnet.Properties.NetworkSecurityGroup)
	}
	if subnet.Properties.RouteTable == nil || subnet.Properties.RouteTable.ID != rt.ID {
		t.Fatalf("expected subnet to reference route table, got %+v", subnet.Properties.RouteTable)
	}
}

func TestPrivateDNSZoneAndRecordSetLifecycle(t *testing.T) {
	srv := newTestServer(t)
	zoneBase := srv.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/privateDnsZones/example.internal"

	var zone PrivateDNSZone
	status := testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(zoneBase), map[string]any{}, &zone)
	if status != http.StatusCreated || zone.Location != "global" {
		t.Fatalf("put private dns zone: status=%d zone=%+v", status, zone)
	}

	aRecordBase := zoneBase + "/A/www"
	var aRecord PrivateDNSRecordSet
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(aRecordBase), map[string]any{
		"properties": map[string]any{
			"ttl":      300,
			"aRecords": []map[string]any{{"ipv4Address": "10.0.0.4"}},
		},
	}, &aRecord)
	if status != http.StatusCreated || len(aRecord.Properties.ARecords) != 1 {
		t.Fatalf("put A record: status=%d record=%+v", status, aRecord)
	}

	cnameBase := zoneBase + "/CNAME/app"
	var cname PrivateDNSRecordSet
	status = testutil.DoJSON(t, "PUT", testutil.WithAPIVersion(cnameBase), map[string]any{
		"properties": map[string]any{
			"ttl":         300,
			"cnameRecord": map[string]any{"cname": "www.example.internal"},
		},
	}, &cname)
	if status != http.StatusCreated || cname.Properties.CnameRecord == nil {
		t.Fatalf("put CNAME record: status=%d record=%+v", status, cname)
	}

	var gotZone PrivateDNSZone
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(zoneBase), nil, &gotZone)
	if status != http.StatusOK || gotZone.Properties.NumberOfRecordSets != 2 {
		t.Fatalf("get zone: status=%d zone=%+v", status, gotZone)
	}

	var aList struct {
		Value []PrivateDNSRecordSet `json:"value"`
	}
	status = testutil.DoJSON(t, "GET", testutil.WithAPIVersion(zoneBase+"/A"), nil, &aList)
	if status != http.StatusOK || len(aList.Value) != 1 {
		t.Fatalf("list A records: status=%d value=%+v", status, aList.Value)
	}

	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(aRecordBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete A record: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(zoneBase), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("delete zone: status=%d", status)
	}
	status = testutil.DoJSON(t, "DELETE", testutil.WithAPIVersion(zoneBase), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("idempotent delete zone: want 204, got %d", status)
	}
}
