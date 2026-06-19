package network

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const networkInterfacesBucket = "network.networkinterfaces"

// NetworkInterface replica la forma estándar de ARM para
// Microsoft.Network/networkInterfaces.
type NetworkInterface struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Location   string                     `json:"location"`
	Tags       map[string]string          `json:"tags,omitempty"`
	Properties NetworkInterfaceProperties `json:"properties"`
}

type NetworkInterfaceProperties struct {
	ProvisioningState string            `json:"provisioningState"`
	IPConfigurations  []IPConfiguration `json:"ipConfigurations"`
}

// IPConfiguration replica "properties.ipConfigurations[]" de una NIC.
type IPConfiguration struct {
	Name       string                    `json:"name"`
	Properties IPConfigurationProperties `json:"properties"`
}

type IPConfigurationProperties struct {
	Subnet                    SubnetReference `json:"subnet"`
	PrivateIPAddress          string          `json:"privateIPAddress"`
	PrivateIPAllocationMethod string          `json:"privateIPAllocationMethod"`
	Primary                   bool            `json:"primary"`
}

// SubnetReference replica el patrón de "referencia por ID" que usa ARM en
// todo el grafo de Microsoft.Network (la subnet, en este caso, vive en otro
// recurso — la VNet — y aquí solo se apunta por su resource ID completo).
type SubnetReference struct {
	ID string `json:"id"`
}

type networkInterfaceRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		IPConfigurations []struct {
			Name       string `json:"name"`
			Properties struct {
				Subnet                    SubnetReference `json:"subnet"`
				PrivateIPAddress          string          `json:"privateIPAddress"`
				PrivateIPAllocationMethod string          `json:"privateIPAllocationMethod"`
			} `json:"properties"`
		} `json:"ipConfigurations"`
	} `json:"properties"`
}

func (s *Service) registerNICs(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkInterfaces",
		s.listNetworkInterfaces)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkInterfaces/{nicName}",
		s.putNetworkInterface)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkInterfaces/{nicName}",
		s.getNetworkInterface)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkInterfaces/{nicName}",
		s.deleteNetworkInterface)
}

func nicKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func nicID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/networkInterfaces/%s", subID, rg, name)
}

func (s *Service) putNetworkInterface(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nicName")

	var req networkInterfaceRequest
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
	if len(req.Properties.IPConfigurations) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.ipConfigurations' es obligatorio y debe tener al menos un elemento")
		return
	}

	ipConfigs := make([]IPConfiguration, 0, len(req.Properties.IPConfigurations))
	for i, reqCfg := range req.Properties.IPConfigurations {
		if strings.TrimSpace(reqCfg.Properties.Subnet.ID) == "" {
			server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
				fmt.Sprintf("ipConfigurations[%d].properties.subnet.id es obligatorio", i))
			return
		}
		subnet, _, found, err := s.findSubnetByID(reqCfg.Properties.Subnet.ID)
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		if !found {
			server.WriteError(w, http.StatusBadRequest, "InvalidSubnetReference",
				fmt.Sprintf("ipConfigurations[%d].properties.subnet.id no apunta a una subnet existente: %s", i, reqCfg.Properties.Subnet.ID))
			return
		}

		allocation := reqCfg.Properties.PrivateIPAllocationMethod
		if strings.TrimSpace(allocation) == "" {
			allocation = "Dynamic"
		}
		privateIP := reqCfg.Properties.PrivateIPAddress
		if allocation != "Static" || privateIP == "" {
			used := s.countNICsOnSubnet(reqCfg.Properties.Subnet.ID)
			ip, err := allocateIP(subnet.Properties.AddressPrefix, used)
			if err != nil {
				server.WriteError(w, http.StatusBadRequest, "InvalidSubnetAddressPrefix",
					fmt.Sprintf("no se pudo derivar una IP privada para la subnet '%s': %v", subnet.Name, err))
				return
			}
			privateIP = ip
		}

		cfgName := reqCfg.Name
		if cfgName == "" {
			cfgName = fmt.Sprintf("ipconfig%d", i+1)
		}
		ipConfigs = append(ipConfigs, IPConfiguration{
			Name: cfgName,
			Properties: IPConfigurationProperties{
				Subnet:                    SubnetReference{ID: reqCfg.Properties.Subnet.ID},
				PrivateIPAddress:          privateIP,
				PrivateIPAllocationMethod: allocation,
				Primary:                   i == 0,
			},
		})
	}

	key := nicKey(subID, rg, name)
	_, found, err := s.getNIC(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	nic := NetworkInterface{
		ID:       nicID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/networkInterfaces",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: NetworkInterfaceProperties{
			ProvisioningState: "Succeeded",
			IPConfigurations:  ipConfigs,
		},
	}

	if err := s.db.Put(networkInterfacesBucket, key, nic); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, nic)
}

func (s *Service) getNetworkInterface(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nicName")

	nic, found, err := s.getNIC(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la network interface '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, nic)
}

func (s *Service) listNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	nics := make([]NetworkInterface, 0)
	err := s.db.List(networkInterfacesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var nic NetworkInterface
		if err := json.Unmarshal(raw, &nic); err != nil {
			return err
		}
		nics = append(nics, nic)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": nics})
}

func (s *Service) deleteNetworkInterface(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nicName")
	key := nicKey(subID, rg, name)

	found, err := s.db.Get(networkInterfacesBucket, key, &NetworkInterface{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(networkInterfacesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getNIC(subID, rg, name string) (NetworkInterface, bool, error) {
	var nic NetworkInterface
	found, err := s.db.Get(networkInterfacesBucket, nicKey(subID, rg, name), &nic)
	return nic, found, err
}

// countNICsOnSubnet cuenta cuántas NICs ya tienen una ipConfiguration
// apuntando a subnetResourceID, para derivar la siguiente IP libre de forma
// determinística (sin necesidad de un pool de IPs real).
func (s *Service) countNICsOnSubnet(subnetResourceID string) int {
	count := 0
	_ = s.db.List(networkInterfacesBucket, "", func(key string, raw []byte) error {
		var nic NetworkInterface
		if err := json.Unmarshal(raw, &nic); err != nil {
			return err
		}
		for _, cfg := range nic.Properties.IPConfigurations {
			if cfg.Properties.Subnet.ID == subnetResourceID {
				count++
			}
		}
		return nil
	})
	return count
}

// FindNICByID resuelve un NIC a partir de su resource ID completo, usado
// por internal/services/compute para validar
// properties.networkProfile.networkInterfaces[].id al crear una VM.
func (s *Service) FindNICByID(nicResourceID string) (NetworkInterface, bool, error) {
	id, ok := server.ParseResourceID(nicResourceID)
	if !ok || id.ResourceType != "networkInterfaces" {
		return NetworkInterface{}, false, nil
	}
	return s.getNIC(id.SubscriptionID, id.ResourceGroup, id.ResourceName)
}

// allocateIP deriva una dirección IP determinística dentro de prefix
// (notación CIDR, p. ej. "10.0.1.0/24"), reservando las primeras 4
// direcciones de la red (como hace Azure real: red, gateway, y dos
// reservadas por la plataforma) y sumando used a partir de ahí.
func allocateIP(prefix string, used int) (string, error) {
	_, ipNet, err := net.ParseCIDR(prefix)
	if err != nil {
		return "", fmt.Errorf("addressPrefix inválido %q: %w", prefix, err)
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("solo se soporta IPv4 (addressPrefix=%q)", prefix)
	}
	offset := uint32(4 + used)
	val := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	val += offset
	candidate := net.IPv4(byte(val>>24), byte(val>>16), byte(val>>8), byte(val))
	if !ipNet.Contains(candidate) {
		return "", fmt.Errorf("la subnet %q no tiene direcciones libres suficientes", prefix)
	}
	return candidate.String(), nil
}
