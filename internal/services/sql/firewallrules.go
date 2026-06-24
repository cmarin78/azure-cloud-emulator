package sql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const firewallRulesBucket = "sql.firewallrules"

// FirewallRule replica el subconjunto relevante de
// Microsoft.Sql/servers/firewallRules que az/Terraform
// (azurerm_mssql_firewall_rule) leen.
type FirewallRule struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties FirewallRuleProperties `json:"properties"`
}

type FirewallRuleProperties struct {
	StartIPAddress string `json:"startIpAddress"`
	EndIPAddress   string `json:"endIpAddress"`
}

type firewallRuleRequest struct {
	Properties FirewallRuleProperties `json:"properties"`
}

func firewallRuleKey(subID, rg, srvName, ruleName string) string {
	return subID + "/" + rg + "/" + srvName + "/" + ruleName
}

func (s *Service) registerFirewallRules(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers/{serverName}/firewallRules"
	mux.HandleFunc("GET "+base, s.listFirewallRules)
	mux.HandleFunc("PUT "+base+"/{ruleName}", s.putFirewallRule)
	mux.HandleFunc("GET "+base+"/{ruleName}", s.getFirewallRuleHandler)
	mux.HandleFunc("DELETE "+base+"/{ruleName}", s.deleteFirewallRule)
}

func (s *Service) putFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	ruleName := r.PathValue("ruleName")

	srv, found, err := s.getServer(subID, rg, srvName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", srvName, rg))
		return
	}

	var req firewallRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.StartIPAddress) == "" || strings.TrimSpace(req.Properties.EndIPAddress) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"los campos 'properties.startIpAddress' y 'properties.endIpAddress' son obligatorios")
		return
	}

	key := firewallRuleKey(subID, rg, srvName, ruleName)
	_, existedBefore, err := s.getFirewallRule(subID, rg, srvName, ruleName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	rule := FirewallRule{
		ID:   srv.ID + "/firewallRules/" + ruleName,
		Name: ruleName,
		Type: "Microsoft.Sql/servers/firewallRules",
		Properties: FirewallRuleProperties{
			StartIPAddress: req.Properties.StartIPAddress,
			EndIPAddress:   req.Properties.EndIPAddress,
		},
	}
	if err := s.db.Put(firewallRulesBucket, key, rule); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rule)
}

func (s *Service) getFirewallRule(subID, rg, srvName, ruleName string) (FirewallRule, bool, error) {
	var rule FirewallRule
	found, err := s.db.Get(firewallRulesBucket, firewallRuleKey(subID, rg, srvName, ruleName), &rule)
	return rule, found, err
}

func (s *Service) getFirewallRuleHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	ruleName := r.PathValue("ruleName")

	rule, found, err := s.getFirewallRule(subID, rg, srvName, ruleName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la firewall rule '%s' no existe en el server '%s'", ruleName, srvName))
		return
	}
	server.WriteJSON(w, http.StatusOK, rule)
}

func (s *Service) listFirewallRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")

	rules := make([]FirewallRule, 0)
	err := s.db.List(firewallRulesBucket, subID+"/"+rg+"/"+srvName+"/", func(_ string, raw []byte) error {
		var rule FirewallRule
		if err := json.Unmarshal(raw, &rule); err != nil {
			return err
		}
		rules = append(rules, rule)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": rules})
}

func (s *Service) deleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	ruleName := r.PathValue("ruleName")
	key := firewallRuleKey(subID, rg, srvName, ruleName)

	found, err := s.db.Get(firewallRulesBucket, key, &FirewallRule{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(firewallRulesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
