// Package network emula el subconjunto de Microsoft.Network que Compute
// necesita como base: virtual networks (con sus subnets como sub-recurso
// anidado, igual que en ARM real) y network interfaces (NICs).
//
// A diferencia de storage accounts o virtual machines, estas mutaciones se
// modelan como síncronas: en Azure real crear/borrar una VNet, subnet o NIC
// normalmente también es rápido y no suele requerir polling explícito en
// los flujos comunes de az CLI/Terraform, así que aquí se responde
// directamente 200/201/204 sin pasar por el helper de LRO (a diferencia de
// virtual machines, ver internal/services/compute, que sí lo necesita).
//
// Las subnets viven anidadas dentro del recurso de la VNet
// (properties.subnets), pero también se exponen como su propio sub-recurso
// ARM (.../virtualNetworks/{vnet}/subnets/{subnet}) porque así es como
// azurerm_subnet y az network vnet subnet las gestionan en la vida real.
package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const virtualNetworksBucket = "network.virtualnetworks"

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Network (virtual networks, subnets, NICs).
type Service struct {
	db *storage.DB
}

// New crea el servicio de red.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta todas las rutas de Microsoft.Network en mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks",
		s.listVirtualNetworks)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}",
		s.putVirtualNetwork)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}",
		s.getVirtualNetwork)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}",
		s.deleteVirtualNetwork)

	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}/subnets",
		s.listSubnets)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}/subnets/{subnetName}",
		s.putSubnet)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}/subnets/{subnetName}",
		s.getSubnet)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{vnetName}/subnets/{subnetName}",
		s.deleteSubnet)

	s.registerNICs(mux)
	s.registerNSGs(mux)
	s.registerPublicIPs(mux)
	s.registerLoadBalancers(mux)
	s.registerRouteTables(mux)
	s.registerPrivateDNS(mux)
}

// AddressSpace replica "properties.addressSpace" de una VNet.
type AddressSpace struct {
	AddressPrefixes []string `json:"addressPrefixes"`
}

// Subnet replica la forma estándar de ARM para
// Microsoft.Network/virtualNetworks/subnets.
type Subnet struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Properties SubnetProperties `json:"properties"`
}

type SubnetProperties struct {
	ProvisioningState    string     `json:"provisioningState"`
	AddressPrefix        string     `json:"addressPrefix"`
	NetworkSecurityGroup *Reference `json:"networkSecurityGroup,omitempty"`
	RouteTable           *Reference `json:"routeTable,omitempty"`
}

// VirtualNetwork replica la forma estándar de ARM para
// Microsoft.Network/virtualNetworks, incluyendo sus subnets anidadas.
type VirtualNetwork struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Properties VirtualNetworkProperties `json:"properties"`
}

type VirtualNetworkProperties struct {
	ProvisioningState string       `json:"provisioningState"`
	AddressSpace      AddressSpace `json:"addressSpace"`
	Subnets           []Subnet     `json:"subnets"`
}

type virtualNetworkRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		AddressSpace AddressSpace `json:"addressSpace"`
	} `json:"properties"`
}

type subnetRequest struct {
	Properties struct {
		AddressPrefix        string     `json:"addressPrefix"`
		NetworkSecurityGroup *Reference `json:"networkSecurityGroup,omitempty"`
		RouteTable           *Reference `json:"routeTable,omitempty"`
	} `json:"properties"`
}

func vnetKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func vnetID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s", subID, rg, name)
}

func (s *Service) putVirtualNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vnetName")

	var req virtualNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio")
		return
	}
	if len(req.Properties.AddressSpace.AddressPrefixes) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.addressSpace.addressPrefixes' es obligatorio y debe tener al menos un prefijo")
		return
	}

	key := vnetKey(subID, rg, name)
	var existing VirtualNetwork
	found, err := s.db.Get(virtualNetworksBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	vnet := VirtualNetwork{
		ID:       vnetID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/virtualNetworks",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: VirtualNetworkProperties{
			ProvisioningState: "Succeeded",
			AddressSpace:      req.Properties.AddressSpace,
			Subnets:           make([]Subnet, 0),
		},
	}
	// Preservar las subnets ya creadas vía el sub-recurso si esto es un
	// update de una VNet existente.
	if found {
		vnet.Properties.Subnets = existing.Properties.Subnets
	}

	if err := s.db.Put(virtualNetworksBucket, key, vnet); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, vnet)
}

func (s *Service) getVirtualNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vnetName")

	vnet, found, err := s.getVNet(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual network '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, vnet)
}

func (s *Service) listVirtualNetworks(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	vnets := make([]VirtualNetwork, 0)
	err := s.db.List(virtualNetworksBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var vnet VirtualNetwork
		if err := json.Unmarshal(raw, &vnet); err != nil {
			return err
		}
		vnets = append(vnets, vnet)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": vnets})
}

// deleteVirtualNetwork es idempotente (204 si no existe) y síncrono. No
// valida que no haya subnets/NICs dependientes (el emulador no modela esa
// cascada, igual de simplificado que el resto del proyecto).
func (s *Service) deleteVirtualNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vnetName")
	key := vnetKey(subID, rg, name)

	found, err := s.db.Get(virtualNetworksBucket, key, &VirtualNetwork{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(virtualNetworksBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getVNet es el helper interno (usado también por nic.go para validar
// referencias a subnets) que busca una VNet por subID/rg/name.
func (s *Service) getVNet(subID, rg, name string) (VirtualNetwork, bool, error) {
	var vnet VirtualNetwork
	found, err := s.db.Get(virtualNetworksBucket, vnetKey(subID, rg, name), &vnet)
	return vnet, found, err
}

func (s *Service) putSubnet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	vnetName := r.PathValue("vnetName")
	subnetName := r.PathValue("subnetName")

	var req subnetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.AddressPrefix) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.addressPrefix' es obligatorio")
		return
	}

	vnet, found, err := s.getVNet(subID, rg, vnetName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual network '%s' no existe en el resource group '%s'", vnetName, rg))
		return
	}

	subnet := Subnet{
		ID:   vnet.ID + "/subnets/" + subnetName,
		Name: subnetName,
		Type: "Microsoft.Network/virtualNetworks/subnets",
		Properties: SubnetProperties{
			ProvisioningState:    "Succeeded",
			AddressPrefix:        req.Properties.AddressPrefix,
			NetworkSecurityGroup: req.Properties.NetworkSecurityGroup,
			RouteTable:           req.Properties.RouteTable,
		},
	}

	existedBefore := false
	replaced := false
	for i, sub := range vnet.Properties.Subnets {
		if sub.Name == subnetName {
			vnet.Properties.Subnets[i] = subnet
			replaced = true
			existedBefore = true
			break
		}
	}
	if !replaced {
		vnet.Properties.Subnets = append(vnet.Properties.Subnets, subnet)
	}

	if err := s.db.Put(virtualNetworksBucket, vnetKey(subID, rg, vnetName), vnet); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, subnet)
}

func (s *Service) getSubnet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	vnetName := r.PathValue("vnetName")
	subnetName := r.PathValue("subnetName")

	vnet, found, err := s.getVNet(subID, rg, vnetName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual network '%s' no existe en el resource group '%s'", vnetName, rg))
		return
	}
	for _, sub := range vnet.Properties.Subnets {
		if sub.Name == subnetName {
			server.WriteJSON(w, http.StatusOK, sub)
			return
		}
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("la subnet '%s' no existe en la virtual network '%s'", subnetName, vnetName))
}

func (s *Service) listSubnets(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	vnetName := r.PathValue("vnetName")

	vnet, found, err := s.getVNet(subID, rg, vnetName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual network '%s' no existe en el resource group '%s'", vnetName, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": vnet.Properties.Subnets})
}

func (s *Service) deleteSubnet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	vnetName := r.PathValue("vnetName")
	subnetName := r.PathValue("subnetName")

	vnet, found, err := s.getVNet(subID, rg, vnetName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	kept := make([]Subnet, 0, len(vnet.Properties.Subnets))
	removed := false
	for _, sub := range vnet.Properties.Subnets {
		if sub.Name == subnetName {
			removed = true
			continue
		}
		kept = append(kept, sub)
	}
	if !removed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	vnet.Properties.Subnets = kept

	if err := s.db.Put(virtualNetworksBucket, vnetKey(subID, rg, vnetName), vnet); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// findSubnetByID busca una subnet a partir de su resource ID completo
// (.../virtualNetworks/{vnet}/subnets/{subnet}), usado por nic.go para
// validar la referencia properties.ipConfigurations[].properties.subnet.id.
func (s *Service) findSubnetByID(subnetResourceID string) (Subnet, VirtualNetwork, bool, error) {
	id, ok := server.ParseResourceID(subnetResourceID)
	if !ok || id.ResourceType != "virtualNetworks" || id.SubResourceType != "subnets" {
		return Subnet{}, VirtualNetwork{}, false, nil
	}
	vnet, found, err := s.getVNet(id.SubscriptionID, id.ResourceGroup, id.ResourceName)
	if err != nil || !found {
		return Subnet{}, VirtualNetwork{}, false, err
	}
	for _, sub := range vnet.Properties.Subnets {
		if sub.Name == id.SubResourceName {
			return sub, vnet, true, nil
		}
	}
	return Subnet{}, vnet, false, nil
}
