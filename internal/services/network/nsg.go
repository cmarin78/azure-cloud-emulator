package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const networkSecurityGroupsBucket = "network.nsgs"

// SecurityRule replica la forma estándar de ARM para
// Microsoft.Network/networkSecurityGroups/securityRules. Igual que las
// subnets dentro de una VNet, vive anidada en properties.securityRules pero
// también se expone como su propio sub-recurso ARM
// (.../networkSecurityGroups/{nsg}/securityRules/{rule}), que es como
// azurerm_network_security_rule las gestiona.
type SecurityRule struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties SecurityRuleProperties `json:"properties"`
}

type SecurityRuleProperties struct {
	ProvisioningState        string `json:"provisioningState"`
	Priority                 int    `json:"priority"`
	Direction                string `json:"direction"`
	Access                   string `json:"access"`
	Protocol                 string `json:"protocol"`
	SourceAddressPrefix      string `json:"sourceAddressPrefix,omitempty"`
	DestinationAddressPrefix string `json:"destinationAddressPrefix,omitempty"`
	SourcePortRange          string `json:"sourcePortRange,omitempty"`
	DestinationPortRange     string `json:"destinationPortRange,omitempty"`
}

// NetworkSecurityGroup replica Microsoft.Network/networkSecurityGroups,
// incluyendo sus securityRules anidadas (mismo patrón que VirtualNetwork/
// Subnets en network.go).
type NetworkSecurityGroup struct {
	ID         string                         `json:"id"`
	Name       string                         `json:"name"`
	Type       string                         `json:"type"`
	Location   string                         `json:"location"`
	Tags       map[string]string              `json:"tags,omitempty"`
	Properties NetworkSecurityGroupProperties `json:"properties"`
}

type NetworkSecurityGroupProperties struct {
	ProvisioningState string         `json:"provisioningState"`
	SecurityRules     []SecurityRule `json:"securityRules"`
}

type networkSecurityGroupRequest struct {
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags,omitempty"`
}

type securityRuleRequest struct {
	Properties struct {
		Priority                 int    `json:"priority"`
		Direction                string `json:"direction"`
		Access                   string `json:"access"`
		Protocol                 string `json:"protocol"`
		SourceAddressPrefix      string `json:"sourceAddressPrefix"`
		DestinationAddressPrefix string `json:"destinationAddressPrefix"`
		SourcePortRange          string `json:"sourcePortRange"`
		DestinationPortRange     string `json:"destinationPortRange"`
	} `json:"properties"`
}

func (s *Service) registerNSGs(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkSecurityGroups"
	mux.HandleFunc("GET "+base, s.listNSGs)
	mux.HandleFunc("PUT "+base+"/{nsgName}", s.putNSG)
	mux.HandleFunc("GET "+base+"/{nsgName}", s.getNSGHandler)
	mux.HandleFunc("DELETE "+base+"/{nsgName}", s.deleteNSG)

	mux.HandleFunc("GET "+base+"/{nsgName}/securityRules", s.listSecurityRules)
	mux.HandleFunc("PUT "+base+"/{nsgName}/securityRules/{ruleName}", s.putSecurityRule)
	mux.HandleFunc("GET "+base+"/{nsgName}/securityRules/{ruleName}", s.getSecurityRule)
	mux.HandleFunc("DELETE "+base+"/{nsgName}/securityRules/{ruleName}", s.deleteSecurityRule)
}

func nsgKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func nsgID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/networkSecurityGroups/%s", subID, rg, name)
}

func (s *Service) putNSG(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nsgName")

	var req networkSecurityGroupRequest
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

	key := nsgKey(subID, rg, name)
	existing, found, err := s.getNSG(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	nsg := NetworkSecurityGroup{
		ID:       nsgID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/networkSecurityGroups",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: NetworkSecurityGroupProperties{
			ProvisioningState: "Succeeded",
			SecurityRules:     make([]SecurityRule, 0),
		},
	}
	// Preservar las reglas ya creadas vía el sub-recurso si esto es un
	// update de un NSG existente (mismo enfoque que putVirtualNetwork con
	// sus subnets).
	if found {
		nsg.Properties.SecurityRules = existing.Properties.SecurityRules
	}

	if err := s.db.Put(networkSecurityGroupsBucket, key, nsg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, nsg)
}

func (s *Service) getNSG(subID, rg, name string) (NetworkSecurityGroup, bool, error) {
	var nsg NetworkSecurityGroup
	found, err := s.db.Get(networkSecurityGroupsBucket, nsgKey(subID, rg, name), &nsg)
	return nsg, found, err
}

func (s *Service) getNSGHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nsgName")

	nsg, found, err := s.getNSG(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el network security group '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, nsg)
}

func (s *Service) listNSGs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	nsgs := make([]NetworkSecurityGroup, 0)
	err := s.db.List(networkSecurityGroupsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var nsg NetworkSecurityGroup
		if err := json.Unmarshal(raw, &nsg); err != nil {
			return err
		}
		nsgs = append(nsgs, nsg)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": nsgs})
}

func (s *Service) deleteNSG(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("nsgName")
	key := nsgKey(subID, rg, name)

	found, err := s.db.Get(networkSecurityGroupsBucket, key, &NetworkSecurityGroup{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(networkSecurityGroupsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) putSecurityRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsgName := r.PathValue("nsgName")
	ruleName := r.PathValue("ruleName")

	var req securityRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if req.Properties.Priority < 100 || req.Properties.Priority > 4096 {
		server.WriteError(w, http.StatusBadRequest, "InvalidParameter",
			"el campo 'properties.priority' es obligatorio y debe estar entre 100 y 4096")
		return
	}
	direction := req.Properties.Direction
	if direction != "Inbound" && direction != "Outbound" {
		server.WriteError(w, http.StatusBadRequest, "InvalidParameter",
			"el campo 'properties.direction' debe ser 'Inbound' u 'Outbound'")
		return
	}
	access := req.Properties.Access
	if access != "Allow" && access != "Deny" {
		server.WriteError(w, http.StatusBadRequest, "InvalidParameter",
			"el campo 'properties.access' debe ser 'Allow' o 'Deny'")
		return
	}
	protocol := req.Properties.Protocol
	if strings.TrimSpace(protocol) == "" {
		protocol = "*"
	}

	nsg, found, err := s.getNSG(subID, rg, nsgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el network security group '%s' no existe en el resource group '%s'", nsgName, rg))
		return
	}

	rule := SecurityRule{
		ID:   nsg.ID + "/securityRules/" + ruleName,
		Name: ruleName,
		Type: "Microsoft.Network/networkSecurityGroups/securityRules",
		Properties: SecurityRuleProperties{
			ProvisioningState:        "Succeeded",
			Priority:                 req.Properties.Priority,
			Direction:                direction,
			Access:                   access,
			Protocol:                 protocol,
			SourceAddressPrefix:      req.Properties.SourceAddressPrefix,
			DestinationAddressPrefix: req.Properties.DestinationAddressPrefix,
			SourcePortRange:          req.Properties.SourcePortRange,
			DestinationPortRange:     req.Properties.DestinationPortRange,
		},
	}

	existedBefore := false
	replaced := false
	for i, existingRule := range nsg.Properties.SecurityRules {
		if existingRule.Name == ruleName {
			nsg.Properties.SecurityRules[i] = rule
			replaced = true
			existedBefore = true
			break
		}
	}
	if !replaced {
		nsg.Properties.SecurityRules = append(nsg.Properties.SecurityRules, rule)
	}

	if err := s.db.Put(networkSecurityGroupsBucket, nsgKey(subID, rg, nsgName), nsg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rule)
}

func (s *Service) getSecurityRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsgName := r.PathValue("nsgName")
	ruleName := r.PathValue("ruleName")

	nsg, found, err := s.getNSG(subID, rg, nsgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el network security group '%s' no existe en el resource group '%s'", nsgName, rg))
		return
	}
	for _, rule := range nsg.Properties.SecurityRules {
		if rule.Name == ruleName {
			server.WriteJSON(w, http.StatusOK, rule)
			return
		}
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("la security rule '%s' no existe en el network security group '%s'", ruleName, nsgName))
}

func (s *Service) listSecurityRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsgName := r.PathValue("nsgName")

	nsg, found, err := s.getNSG(subID, rg, nsgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el network security group '%s' no existe en el resource group '%s'", nsgName, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": nsg.Properties.SecurityRules})
}

func (s *Service) deleteSecurityRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsgName := r.PathValue("nsgName")
	ruleName := r.PathValue("ruleName")

	nsg, found, err := s.getNSG(subID, rg, nsgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	kept := make([]SecurityRule, 0, len(nsg.Properties.SecurityRules))
	removed := false
	for _, rule := range nsg.Properties.SecurityRules {
		if rule.Name == ruleName {
			removed = true
			continue
		}
		kept = append(kept, rule)
	}
	if !removed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	nsg.Properties.SecurityRules = kept

	if err := s.db.Put(networkSecurityGroupsBucket, nsgKey(subID, rg, nsgName), nsg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
