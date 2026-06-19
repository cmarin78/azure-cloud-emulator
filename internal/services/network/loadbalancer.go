package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const loadBalancersBucket = "network.loadbalancers"

// LoadBalancer replica Microsoft.Network/loadBalancers. Igual que NSGs y
// VNets, sus colecciones anidadas (frontendIPConfigurations,
// backendAddressPools, loadBalancingRules, probes) viven como slices dentro
// de properties y se reemplazan completas en cada PUT del recurso padre —
// a diferencia de las subnets/securityRules, este emulador no expone estas
// colecciones como sub-recursos ARM independientes porque azurerm las
// gestiona siempre inline dentro de azurerm_lb / azurerm_lb_rule usando el
// id del load balancer, no rutas PUT propias.
//
// No se valida integridad referencial entre frontendIPConfiguration.id,
// backendAddressPool.id o publicIPAddress.id: mismo criterio de diseño que
// el resto del proyecto (ver findSubnetByID, FindNICByID).
type LoadBalancer struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Location   string                 `json:"location"`
	Tags       map[string]string      `json:"tags,omitempty"`
	SKU        LoadBalancerSKU        `json:"sku"`
	Properties LoadBalancerProperties `json:"properties"`
}

type LoadBalancerSKU struct {
	Name string `json:"name"`
}

type LoadBalancerProperties struct {
	ProvisioningState        string                    `json:"provisioningState"`
	FrontendIPConfigurations []FrontendIPConfiguration `json:"frontendIPConfigurations"`
	BackendAddressPools      []BackendAddressPool      `json:"backendAddressPools"`
	LoadBalancingRules       []LoadBalancingRule       `json:"loadBalancingRules"`
	Probes                   []LoadBalancerProbe       `json:"probes"`
}

type FrontendIPConfiguration struct {
	ID         string                            `json:"id"`
	Name       string                            `json:"name"`
	Properties FrontendIPConfigurationProperties `json:"properties"`
}

type FrontendIPConfigurationProperties struct {
	PublicIPAddress  *Reference `json:"publicIPAddress,omitempty"`
	PrivateIPAddress string     `json:"privateIPAddress,omitempty"`
}

// Reference es el patrón genérico "{ id: string }" que ARM usa para
// referenciar otro recurso por id, ya empleado como SubnetReference en
// nic.go. Se generaliza aquí porque load balancers lo necesitan para
// publicIPAddress, subnet, backendAddressPool y frontendIPConfiguration.
type Reference struct {
	ID string `json:"id"`
}

type BackendAddressPool struct {
	ID         string                       `json:"id"`
	Name       string                       `json:"name"`
	Properties BackendAddressPoolProperties `json:"properties"`
}

type BackendAddressPoolProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

type LoadBalancingRule struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Properties LoadBalancingRuleProperties `json:"properties"`
}

type LoadBalancingRuleProperties struct {
	FrontendIPConfiguration *Reference `json:"frontendIPConfiguration,omitempty"`
	BackendAddressPool      *Reference `json:"backendAddressPool,omitempty"`
	Protocol                string     `json:"protocol"`
	FrontendPort            int        `json:"frontendPort"`
	BackendPort             int        `json:"backendPort"`
}

type LoadBalancerProbe struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Properties LoadBalancerProbeProperties `json:"properties"`
}

type LoadBalancerProbeProperties struct {
	Protocol          string `json:"protocol"`
	Port              int    `json:"port"`
	RequestPath       string `json:"requestPath,omitempty"`
	IntervalInSeconds int    `json:"intervalInSeconds,omitempty"`
}

type loadBalancerRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	SKU        LoadBalancerSKU   `json:"sku"`
	Properties struct {
		FrontendIPConfigurations []struct {
			Name       string `json:"name"`
			Properties struct {
				PublicIPAddress  *Reference `json:"publicIPAddress,omitempty"`
				PrivateIPAddress string     `json:"privateIPAddress,omitempty"`
			} `json:"properties"`
		} `json:"frontendIPConfigurations"`
		BackendAddressPools []struct {
			Name string `json:"name"`
		} `json:"backendAddressPools"`
		LoadBalancingRules []struct {
			Name       string `json:"name"`
			Properties struct {
				FrontendIPConfiguration *Reference `json:"frontendIPConfiguration,omitempty"`
				BackendAddressPool      *Reference `json:"backendAddressPool,omitempty"`
				Protocol                string     `json:"protocol"`
				FrontendPort            int        `json:"frontendPort"`
				BackendPort             int        `json:"backendPort"`
			} `json:"properties"`
		} `json:"loadBalancingRules"`
		Probes []struct {
			Name       string `json:"name"`
			Properties struct {
				Protocol          string `json:"protocol"`
				Port              int    `json:"port"`
				RequestPath       string `json:"requestPath,omitempty"`
				IntervalInSeconds int    `json:"intervalInSeconds,omitempty"`
			} `json:"properties"`
		} `json:"probes"`
	} `json:"properties"`
}

func (s *Service) registerLoadBalancers(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/loadBalancers"
	mux.HandleFunc("GET "+base, s.listLoadBalancers)
	mux.HandleFunc("PUT "+base+"/{lbName}", s.putLoadBalancer)
	mux.HandleFunc("GET "+base+"/{lbName}", s.getLoadBalancerHandler)
	mux.HandleFunc("DELETE "+base+"/{lbName}", s.deleteLoadBalancer)
}

func loadBalancerKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func loadBalancerID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/loadBalancers/%s", subID, rg, name)
}

func (s *Service) putLoadBalancer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("lbName")

	var req loadBalancerRequest
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

	skuName := req.SKU.Name
	if strings.TrimSpace(skuName) == "" {
		skuName = "Basic"
	}

	id := loadBalancerID(subID, rg, name)

	frontends := make([]FrontendIPConfiguration, 0, len(req.Properties.FrontendIPConfigurations))
	for _, f := range req.Properties.FrontendIPConfigurations {
		frontends = append(frontends, FrontendIPConfiguration{
			ID:   id + "/frontendIPConfigurations/" + f.Name,
			Name: f.Name,
			Properties: FrontendIPConfigurationProperties{
				PublicIPAddress:  f.Properties.PublicIPAddress,
				PrivateIPAddress: f.Properties.PrivateIPAddress,
			},
		})
	}

	pools := make([]BackendAddressPool, 0, len(req.Properties.BackendAddressPools))
	for _, p := range req.Properties.BackendAddressPools {
		pools = append(pools, BackendAddressPool{
			ID:         id + "/backendAddressPools/" + p.Name,
			Name:       p.Name,
			Properties: BackendAddressPoolProperties{ProvisioningState: "Succeeded"},
		})
	}

	rules := make([]LoadBalancingRule, 0, len(req.Properties.LoadBalancingRules))
	for _, lr := range req.Properties.LoadBalancingRules {
		protocol := lr.Properties.Protocol
		if strings.TrimSpace(protocol) == "" {
			protocol = "Tcp"
		}
		rules = append(rules, LoadBalancingRule{
			ID:   id + "/loadBalancingRules/" + lr.Name,
			Name: lr.Name,
			Properties: LoadBalancingRuleProperties{
				FrontendIPConfiguration: lr.Properties.FrontendIPConfiguration,
				BackendAddressPool:      lr.Properties.BackendAddressPool,
				Protocol:                protocol,
				FrontendPort:            lr.Properties.FrontendPort,
				BackendPort:             lr.Properties.BackendPort,
			},
		})
	}

	probes := make([]LoadBalancerProbe, 0, len(req.Properties.Probes))
	for _, p := range req.Properties.Probes {
		protocol := p.Properties.Protocol
		if strings.TrimSpace(protocol) == "" {
			protocol = "Tcp"
		}
		probes = append(probes, LoadBalancerProbe{
			ID:   id + "/probes/" + p.Name,
			Name: p.Name,
			Properties: LoadBalancerProbeProperties{
				Protocol:          protocol,
				Port:              p.Properties.Port,
				RequestPath:       p.Properties.RequestPath,
				IntervalInSeconds: p.Properties.IntervalInSeconds,
			},
		})
	}

	key := loadBalancerKey(subID, rg, name)
	found, err := s.db.Get(loadBalancersBucket, key, &LoadBalancer{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	lb := LoadBalancer{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.Network/loadBalancers",
		Location: req.Location,
		Tags:     req.Tags,
		SKU:      LoadBalancerSKU{Name: skuName},
		Properties: LoadBalancerProperties{
			ProvisioningState:        "Succeeded",
			FrontendIPConfigurations: frontends,
			BackendAddressPools:      pools,
			LoadBalancingRules:       rules,
			Probes:                   probes,
		},
	}

	if err := s.db.Put(loadBalancersBucket, key, lb); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, lb)
}

func (s *Service) getLoadBalancer(subID, rg, name string) (LoadBalancer, bool, error) {
	var lb LoadBalancer
	found, err := s.db.Get(loadBalancersBucket, loadBalancerKey(subID, rg, name), &lb)
	return lb, found, err
}

func (s *Service) getLoadBalancerHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("lbName")

	lb, found, err := s.getLoadBalancer(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el load balancer '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, lb)
}

func (s *Service) listLoadBalancers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	lbs := make([]LoadBalancer, 0)
	err := s.db.List(loadBalancersBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var lb LoadBalancer
		if err := json.Unmarshal(raw, &lb); err != nil {
			return err
		}
		lbs = append(lbs, lb)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": lbs})
}

func (s *Service) deleteLoadBalancer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("lbName")
	key := loadBalancerKey(subID, rg, name)

	found, err := s.db.Get(loadBalancersBucket, key, &LoadBalancer{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(loadBalancersBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
