// privateendpoints.go implementa Microsoft.Network/privateEndpoints y
// Microsoft.Network/privateLinkServices en el paquete network existente.
// Ambos recursos son parte de Microsoft.Network, lo que los hace ciudadanos
// naturales de este paquete (igual que NSGs, LoadBalancers, RouteTables).
package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const (
	pesBucket = "network.privateendpoints"
	plsBucket = "network.privatelinkservices"
)

// ── Private Endpoints ──────────────────────────────────────────────────────

// PrivateEndpoint replica Microsoft.Network/privateEndpoints tal como
// azurerm_private_endpoint lo lee.
type PrivateEndpoint struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties PEProperties      `json:"properties"`
}

// PEProperties incluye los campos que azurerm_private_endpoint lee en Refresh.
type PEProperties struct {
	ProvisioningState            string                         `json:"provisioningState"`
	PrivateLinkServiceConnections []PLServiceConnection          `json:"privateLinkServiceConnections,omitempty"`
	Subnet                       *SubnetRef                     `json:"subnet,omitempty"`
	NetworkInterfaces            []NICRef                       `json:"networkInterfaces,omitempty"`
	CustomNetworkInterfaceName   string                         `json:"customNetworkInterfaceName,omitempty"`
	IPConfigurations             []PEIPConfig                   `json:"ipConfigurations,omitempty"`
}

// PLServiceConnection es la conexión hacia el Private Link Service o recurso Azure.
type PLServiceConnection struct {
	Name       string              `json:"name"`
	ID         string              `json:"id,omitempty"`
	Properties PLSConnectionProps  `json:"properties"`
}

type PLSConnectionProps struct {
	PrivateLinkServiceID        string   `json:"privateLinkServiceId"`
	GroupIDs                    []string `json:"groupIds,omitempty"`
	RequestMessage              string   `json:"requestMessage,omitempty"`
	PrivateLinkServiceConnectionState PLSConnectionState `json:"privateLinkServiceConnectionState"`
}

type PLSConnectionState struct {
	Status          string `json:"status"`
	Description     string `json:"description,omitempty"`
	ActionsRequired string `json:"actionsRequired,omitempty"`
}

type SubnetRef struct {
	ID string `json:"id"`
}

type NICRef struct {
	ID string `json:"id"`
}

type PEIPConfig struct {
	Name       string `json:"name,omitempty"`
	Properties struct {
		GroupID         string `json:"groupId,omitempty"`
		MemberName      string `json:"memberName,omitempty"`
		PrivateIPAddress string `json:"privateIPAddress,omitempty"`
	} `json:"properties,omitempty"`
}

type peRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		PrivateLinkServiceConnections []PLServiceConnection `json:"privateLinkServiceConnections,omitempty"`
		Subnet                        *SubnetRef            `json:"subnet,omitempty"`
		CustomNetworkInterfaceName    string                `json:"customNetworkInterfaceName,omitempty"`
		IPConfigurations              []PEIPConfig          `json:"ipConfigurations,omitempty"`
	} `json:"properties"`
}

func peKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func peID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/privateEndpoints/%s", sub, rg, name)
}

// fakeNICID devuelve un ID de NIC determinista para el private endpoint.
func fakeNICID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/networkInterfaces/%s.nic.00000000-0000-0000-0000-000000000000", sub, rg, strings.ToLower(name))
}

func (s *Service) registerPrivateEndpoints(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/privateEndpoints"
	mux.HandleFunc("GET "+base, s.listPrivateEndpoints)
	mux.HandleFunc("PUT "+base+"/{peName}", s.putPrivateEndpoint)
	mux.HandleFunc("GET "+base+"/{peName}", s.getPrivateEndpoint)
	mux.HandleFunc("DELETE "+base+"/{peName}", s.deletePrivateEndpoint)
	// privateDnsZoneGroups: azurerm_private_endpoint lo gestiona como sub-recurso
	mux.HandleFunc("GET "+base+"/{peName}/privateDnsZoneGroups", s.listDNSZoneGroups)
	mux.HandleFunc("PUT "+base+"/{peName}/privateDnsZoneGroups/{groupName}", s.putDNSZoneGroup)
	mux.HandleFunc("GET "+base+"/{peName}/privateDnsZoneGroups/{groupName}", s.getDNSZoneGroup)
	mux.HandleFunc("DELETE "+base+"/{peName}/privateDnsZoneGroups/{groupName}", s.deleteDNSZoneGroup)
}

func (s *Service) putPrivateEndpoint(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("peName")

	var req peRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := peKey(sub, rg, name)
	var existing PrivateEndpoint
	found, _ := s.db.Get(pesBucket, key, &existing)

	// Asegurar que cada conexión tenga un estado "Approved" por defecto.
	conns := req.Properties.PrivateLinkServiceConnections
	for i := range conns {
		if conns[i].Properties.PrivateLinkServiceConnectionState.Status == "" {
			conns[i].Properties.PrivateLinkServiceConnectionState = PLSConnectionState{
				Status:          "Approved",
				Description:     "Auto-approved",
				ActionsRequired: "None",
			}
		}
		if conns[i].ID == "" {
			conns[i].ID = peID(sub, rg, name) + "/privateLinkServiceConnections/" + conns[i].Name
		}
	}

	pe := PrivateEndpoint{
		ID:       peID(sub, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/privateEndpoints",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: PEProperties{
			ProvisioningState:             "Succeeded",
			PrivateLinkServiceConnections: conns,
			Subnet:                        req.Properties.Subnet,
			CustomNetworkInterfaceName:    req.Properties.CustomNetworkInterfaceName,
			IPConfigurations:              req.Properties.IPConfigurations,
			NetworkInterfaces:             []NICRef{{ID: fakeNICID(sub, rg, name)}},
		},
	}
	if err := s.db.Put(pesBucket, key, pe); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, pe)
}

func (s *Service) getPrivateEndpoint(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("peName")
	var pe PrivateEndpoint
	found, err := s.db.Get(pesBucket, peKey(sub, rg, name), &pe)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("private endpoint '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, pe)
}

func (s *Service) listPrivateEndpoints(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	items := make([]PrivateEndpoint, 0)
	_ = s.db.List(pesBucket, sub+"/"+rg+"/", func(_ string, raw []byte) error {
		var pe PrivateEndpoint
		if err := json.Unmarshal(raw, &pe); err == nil {
			items = append(items, pe)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deletePrivateEndpoint(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("peName")
	_ = s.db.Delete(pesBucket, peKey(sub, rg, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── privateDnsZoneGroups ────────────────────────────────────────────────────

const dnsZoneGroupBucket = "network.pednszonegroups"

// DNSZoneGroup replica azurerm_private_dns_zone_group dentro de un PE.
type DNSZoneGroup struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Properties DNSZoneGroupProps  `json:"properties"`
}

type DNSZoneGroupProps struct {
	ProvisioningState string         `json:"provisioningState"`
	PrivateDNSZoneConfigs []DNSZoneCfg `json:"privateDnsZoneConfigs,omitempty"`
}

type DNSZoneCfg struct {
	Name       string `json:"name"`
	Properties struct {
		PrivateDNSZoneID string `json:"privateDnsZoneId"`
	} `json:"properties"`
}

func dnsGroupKey(sub, rg, peName, groupName string) string {
	return sub + "/" + rg + "/" + peName + "/" + groupName
}
func dnsGroupID(sub, rg, peName, groupName string) string {
	return peID(sub, rg, peName) + "/privateDnsZoneGroups/" + groupName
}

func (s *Service) putDNSZoneGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	peName := r.PathValue("peName")
	groupName := r.PathValue("groupName")

	var req struct {
		Properties DNSZoneGroupProps `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	key := dnsGroupKey(sub, rg, peName, groupName)
	var existing DNSZoneGroup
	found, _ := s.db.Get(dnsZoneGroupBucket, key, &existing)

	grp := DNSZoneGroup{
		ID:   dnsGroupID(sub, rg, peName, groupName),
		Name: groupName,
		Type: "Microsoft.Network/privateEndpoints/privateDnsZoneGroups",
		Properties: DNSZoneGroupProps{
			ProvisioningState:   "Succeeded",
			PrivateDNSZoneConfigs: req.Properties.PrivateDNSZoneConfigs,
		},
	}
	if err := s.db.Put(dnsZoneGroupBucket, key, grp); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, grp)
}

func (s *Service) getDNSZoneGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	peName := r.PathValue("peName")
	groupName := r.PathValue("groupName")
	var grp DNSZoneGroup
	found, err := s.db.Get(dnsZoneGroupBucket, dnsGroupKey(sub, rg, peName, groupName), &grp)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("privateDnsZoneGroup '%s' no encontrado", groupName))
		return
	}
	server.WriteJSON(w, http.StatusOK, grp)
}

func (s *Service) listDNSZoneGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	peName := r.PathValue("peName")
	items := make([]DNSZoneGroup, 0)
	_ = s.db.List(dnsZoneGroupBucket, sub+"/"+rg+"/"+peName+"/", func(_ string, raw []byte) error {
		var grp DNSZoneGroup
		if err := json.Unmarshal(raw, &grp); err == nil {
			items = append(items, grp)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteDNSZoneGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	peName := r.PathValue("peName")
	groupName := r.PathValue("groupName")
	_ = s.db.Delete(dnsZoneGroupBucket, dnsGroupKey(sub, rg, peName, groupName))
	w.WriteHeader(http.StatusNoContent)
}

// ── Private Link Services ──────────────────────────────────────────────────

// PrivateLinkService replica Microsoft.Network/privateLinkServices.
type PrivateLinkService struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties PLSProperties       `json:"properties"`
}

type PLSProperties struct {
	ProvisioningState          string                `json:"provisioningState"`
	Alias                      string                `json:"alias"`
	LoadBalancerFrontendIPConfigurations []FrontendIPConfigRef `json:"loadBalancerFrontendIpConfigurations,omitempty"`
	IPConfigurations           []PLSIPConfig         `json:"ipConfigurations,omitempty"`
	NetworkInterfaces          []NICRef              `json:"networkInterfaces,omitempty"`
	PrivateEndpointConnections []PLSPEConnection     `json:"privateEndpointConnections,omitempty"`
	Visibility                 *PLSVisibility        `json:"visibility,omitempty"`
	AutoApproval               *PLSAutoApproval      `json:"autoApproval,omitempty"`
	EnableProxyProtocol        bool                  `json:"enableProxyProtocol,omitempty"`
}

type FrontendIPConfigRef struct {
	ID string `json:"id"`
}

type PLSIPConfig struct {
	Name       string `json:"name"`
	Properties struct {
		PrivateIPAllocationMethod string     `json:"privateIpAllocationMethod,omitempty"`
		PrivateIPAddress          string     `json:"privateIpAddress,omitempty"`
		Subnet                    *SubnetRef `json:"subnet,omitempty"`
		Primary                   bool       `json:"primary,omitempty"`
	} `json:"properties"`
}

type PLSPEConnection struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	Properties struct {
		ProvisioningState               string             `json:"provisioningState"`
		PrivateLinkServiceConnectionState PLSConnectionState `json:"privateLinkServiceConnectionState"`
	} `json:"properties"`
}

type PLSVisibility struct {
	Subscriptions []string `json:"subscriptions,omitempty"`
}

type PLSAutoApproval struct {
	Subscriptions []string `json:"subscriptions,omitempty"`
}

type plsRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		LoadBalancerFrontendIPConfigurations []FrontendIPConfigRef `json:"loadBalancerFrontendIpConfigurations,omitempty"`
		IPConfigurations                    []PLSIPConfig          `json:"ipConfigurations,omitempty"`
		Visibility                          *PLSVisibility         `json:"visibility,omitempty"`
		AutoApproval                        *PLSAutoApproval       `json:"autoApproval,omitempty"`
		EnableProxyProtocol                 bool                   `json:"enableProxyProtocol,omitempty"`
	} `json:"properties"`
}

func plsKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func plsID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/privateLinkServices/%s", sub, rg, name)
}

// fakePLSAlias devuelve un alias determinista para el Private Link Service.
func fakePLSAlias(name string) string {
	return fmt.Sprintf("%s.00000000-0000-0000-0000-000000000000.eastus.azure.privatelinkservice", strings.ToLower(name))
}

func (s *Service) registerPrivateLinkServices(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/privateLinkServices"
	mux.HandleFunc("GET "+base, s.listPLS)
	mux.HandleFunc("PUT "+base+"/{plsName}", s.putPLS)
	mux.HandleFunc("GET "+base+"/{plsName}", s.getPLS)
	mux.HandleFunc("DELETE "+base+"/{plsName}", s.deletePLS)
	// privateEndpointConnections sub-resource (read-only list + individual GET)
	mux.HandleFunc("GET "+base+"/{plsName}/privateEndpointConnections", s.listPLSConnections)
	mux.HandleFunc("GET "+base+"/{plsName}/privateEndpointConnections/{connName}", s.getPLSConnection)
	mux.HandleFunc("DELETE "+base+"/{plsName}/privateEndpointConnections/{connName}", s.deletePLSConnection)
}

func (s *Service) putPLS(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("plsName")

	var req plsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := plsKey(sub, rg, name)
	var existing PrivateLinkService
	found, _ := s.db.Get(plsBucket, key, &existing)

	pls := PrivateLinkService{
		ID:       plsID(sub, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/privateLinkServices",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: PLSProperties{
			ProvisioningState:                   "Succeeded",
			Alias:                               fakePLSAlias(name),
			LoadBalancerFrontendIPConfigurations: req.Properties.LoadBalancerFrontendIPConfigurations,
			IPConfigurations:                    req.Properties.IPConfigurations,
			Visibility:                          req.Properties.Visibility,
			AutoApproval:                        req.Properties.AutoApproval,
			EnableProxyProtocol:                 req.Properties.EnableProxyProtocol,
			NetworkInterfaces:                   []NICRef{},
			PrivateEndpointConnections:          []PLSPEConnection{},
		},
	}
	if err := s.db.Put(plsBucket, key, pls); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, pls)
}

func (s *Service) getPLS(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("plsName")
	var pls PrivateLinkService
	found, err := s.db.Get(plsBucket, plsKey(sub, rg, name), &pls)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("private link service '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, pls)
}

func (s *Service) listPLS(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	items := make([]PrivateLinkService, 0)
	_ = s.db.List(plsBucket, sub+"/"+rg+"/", func(_ string, raw []byte) error {
		var pls PrivateLinkService
		if err := json.Unmarshal(raw, &pls); err == nil {
			items = append(items, pls)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deletePLS(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("plsName")
	_ = s.db.Delete(plsBucket, plsKey(sub, rg, name))
	w.WriteHeader(http.StatusNoContent)
}

// privateEndpointConnections: stubs que azurerm puede llamar en Refresh.
func (s *Service) listPLSConnections(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

func (s *Service) getPLSConnection(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound", "connection not found")
}

func (s *Service) deletePLSConnection(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
