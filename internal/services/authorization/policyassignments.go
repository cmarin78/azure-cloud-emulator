package authorization

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const policyAssignmentBucket = "authorization.policyAssignments"

// PolicyAssignment es la representación ARM de Microsoft.Authorization/policyAssignments.
// El scope puede ser a nivel de suscripción o de resource group.
type PolicyAssignment struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Properties PolicyAssignmentProperties `json:"properties"`
}

// PolicyAssignmentProperties son los campos que el provider azurerm lee en Read.
type PolicyAssignmentProperties struct {
	PolicyDefinitionID string          `json:"policyDefinitionId,omitempty"`
	DisplayName        string          `json:"displayName,omitempty"`
	Description        string          `json:"description,omitempty"`
	EnforcementMode    string          `json:"enforcementMode,omitempty"`
	Parameters         json.RawMessage `json:"parameters,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
	Scope              string          `json:"scope,omitempty"`
}

// policyAssignmentRequest es el body del PUT que manda el provider.
type policyAssignmentRequest struct {
	Properties PolicyAssignmentProperties `json:"properties"`
}

func policyAssignKey(scope, name string) string { return scope + "/" + name }

func (s *Service) registerPolicyAssignments(mux *http.ServeMux) {
	// Scope de suscripción
	subBase := "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyAssignments"
	mux.HandleFunc("GET "+subBase, s.listPolicyAssignmentsSub)
	mux.HandleFunc("PUT "+subBase+"/{policyAssignmentName}", s.putPolicyAssignmentSub)
	mux.HandleFunc("GET "+subBase+"/{policyAssignmentName}", s.getPolicyAssignmentSub)
	mux.HandleFunc("DELETE "+subBase+"/{policyAssignmentName}", s.deletePolicyAssignmentSub)

	// Scope de resource group
	rgBase := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Authorization/policyAssignments"
	mux.HandleFunc("GET "+rgBase, s.listPolicyAssignmentsRG)
	mux.HandleFunc("PUT "+rgBase+"/{policyAssignmentName}", s.putPolicyAssignmentRG)
	mux.HandleFunc("GET "+rgBase+"/{policyAssignmentName}", s.getPolicyAssignmentRG)
	mux.HandleFunc("DELETE "+rgBase+"/{policyAssignmentName}", s.deletePolicyAssignmentRG)
}

// ---------- helpers ----------

func (s *Service) upsertPolicyAssignment(
	w http.ResponseWriter, r *http.Request,
	scope, name string,
) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	var req policyAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	id := scope + "/providers/Microsoft.Authorization/policyAssignments/" + name

	mode := req.Properties.EnforcementMode
	if mode == "" {
		mode = "Default"
	}

	assignment := PolicyAssignment{
		ID:   id,
		Name: name,
		Type: "Microsoft.Authorization/policyAssignments",
		Properties: PolicyAssignmentProperties{
			PolicyDefinitionID: req.Properties.PolicyDefinitionID,
			DisplayName:        req.Properties.DisplayName,
			Description:        req.Properties.Description,
			EnforcementMode:    mode,
			Parameters:         req.Properties.Parameters,
			Metadata:           req.Properties.Metadata,
			Scope:              scope,
		},
	}

	key := policyAssignKey(scope, name)
	var existing PolicyAssignment
	existedBefore, err := s.db.Get(policyAssignmentBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Put(policyAssignmentBucket, key, assignment); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, assignment)
}

func (s *Service) fetchPolicyAssignment(
	w http.ResponseWriter, r *http.Request,
	scope, name string,
) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	var a PolicyAssignment
	found, err := s.db.Get(policyAssignmentBucket, policyAssignKey(scope, name), &a)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "PolicyAssignmentNotFound",
			fmt.Sprintf("la asignación de política '%s' no existe", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, a)
}

func (s *Service) removePolicyAssignment(
	w http.ResponseWriter, r *http.Request,
	scope, name string,
) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	key := policyAssignKey(scope, name)
	found, err := s.db.Get(policyAssignmentBucket, key, &PolicyAssignment{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(policyAssignmentBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) listPolicyAssignments(
	w http.ResponseWriter, r *http.Request,
	prefix string,
) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	assignments := make([]PolicyAssignment, 0)
	err := s.db.List(policyAssignmentBucket, prefix, func(_ string, raw []byte) error {
		var a PolicyAssignment
		if err := json.Unmarshal(raw, &a); err != nil {
			return err
		}
		assignments = append(assignments, a)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": assignments})
}

// ---------- subscription-scope handlers ----------

func (s *Service) putPolicyAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID
	s.upsertPolicyAssignment(w, r, scope, name)
}

func (s *Service) getPolicyAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID
	s.fetchPolicyAssignment(w, r, scope, name)
}

func (s *Service) deletePolicyAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID
	s.removePolicyAssignment(w, r, scope, name)
}

func (s *Service) listPolicyAssignmentsSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	prefix := "/subscriptions/" + subID + "/"
	s.listPolicyAssignments(w, r, prefix)
}

// ---------- resource-group-scope handlers ----------

func (s *Service) putPolicyAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID + "/resourceGroups/" + rg
	s.upsertPolicyAssignment(w, r, scope, name)
}

func (s *Service) getPolicyAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID + "/resourceGroups/" + rg
	s.fetchPolicyAssignment(w, r, scope, name)
}

func (s *Service) deletePolicyAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("policyAssignmentName")
	scope := "/subscriptions/" + subID + "/resourceGroups/" + rg
	s.removePolicyAssignment(w, r, scope, name)
}

func (s *Service) listPolicyAssignmentsRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	prefix := "/subscriptions/" + subID + "/resourceGroups/" + rg + "/"
	s.listPolicyAssignments(w, r, prefix)
}
