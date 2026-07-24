package authorization

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const policyDefinitionBucket = "authorization.policyDefinitions"

// PolicyDefinition es la representación ARM de Microsoft.Authorization/policyDefinitions.
// PolicyRule y Parameters se almacenan como valores JSON crudos (json.RawMessage)
// para preservar el shape que manda el provider sin re-serializar.
type PolicyDefinition struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Properties PolicyDefinitionProperties `json:"properties"`
}

// PolicyDefinitionProperties son los campos que el provider azurerm_policy_definition
// lee en su Read.
type PolicyDefinitionProperties struct {
	PolicyType  string          `json:"policyType,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	PolicyRule  json.RawMessage `json:"policyRule,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// policyDefinitionRequest es el body del PUT que manda el provider.
type policyDefinitionRequest struct {
	Properties PolicyDefinitionProperties `json:"properties"`
}

func policyDefKey(subID, name string) string { return subID + "/" + name }

func (s *Service) registerPolicyDefinitions(mux *http.ServeMux) {
	// Suscripción: CRUD
	sub := "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyDefinitions"
	mux.HandleFunc("GET "+sub, s.listPolicyDefinitions)
	mux.HandleFunc("PUT "+sub+"/{policyDefinitionName}", s.putPolicyDefinition)
	mux.HandleFunc("GET "+sub+"/{policyDefinitionName}", s.getPolicyDefinitionHandler)
	mux.HandleFunc("DELETE "+sub+"/{policyDefinitionName}", s.deletePolicyDefinition)

	// Built-in: devolver un documento vacío para que el provider pueda hacer
	// lookups sin fallar (ej. azurerm_policy_definition data source).
	builtIn := "/providers/Microsoft.Authorization/policyDefinitions"
	mux.HandleFunc("GET "+builtIn, s.listBuiltInPolicyDefinitions)
	mux.HandleFunc("GET "+builtIn+"/{policyDefinitionName}", s.getBuiltInPolicyDefinition)
}

func (s *Service) putPolicyDefinition(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyDefinitionName")

	var req policyDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	id := "/subscriptions/" + subID +
		"/providers/Microsoft.Authorization/policyDefinitions/" + name

	policyType := req.Properties.PolicyType
	if policyType == "" {
		policyType = "Custom"
	}
	mode := req.Properties.Mode
	if mode == "" {
		mode = "All"
	}

	def := PolicyDefinition{
		ID:   id,
		Name: name,
		Type: "Microsoft.Authorization/policyDefinitions",
		Properties: PolicyDefinitionProperties{
			PolicyType:  policyType,
			Mode:        mode,
			DisplayName: req.Properties.DisplayName,
			Description: req.Properties.Description,
			PolicyRule:  req.Properties.PolicyRule,
			Parameters:  req.Properties.Parameters,
			Metadata:    req.Properties.Metadata,
		},
	}

	key := policyDefKey(subID, name)
	_, existedBefore, err := s.getPolicyDefinition(subID, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Put(policyDefinitionBucket, key, def); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, def)
}

func (s *Service) getPolicyDefinition(subID, name string) (PolicyDefinition, bool, error) {
	var d PolicyDefinition
	found, err := s.db.Get(policyDefinitionBucket, policyDefKey(subID, name), &d)
	return d, found, err
}

func (s *Service) getPolicyDefinitionHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyDefinitionName")

	d, found, err := s.getPolicyDefinition(subID, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "PolicyDefinitionNotFound",
			fmt.Sprintf("la definición de política '%s' no existe", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, d)
}

func (s *Service) listPolicyDefinitions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")

	defs := make([]PolicyDefinition, 0)
	err := s.db.List(policyDefinitionBucket, subID+"/", func(_ string, raw []byte) error {
		var d PolicyDefinition
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		defs = append(defs, d)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": defs})
}

func (s *Service) deletePolicyDefinition(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("policyDefinitionName")
	key := policyDefKey(subID, name)

	found, err := s.db.Get(policyDefinitionBucket, key, &PolicyDefinition{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(policyDefinitionBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Built-in handlers: devuelven listas/objetos vacíos para que el provider no falle
// en lookups de políticas predefinidas de Azure.

func (s *Service) listBuiltInPolicyDefinitions(w http.ResponseWriter, r *http.Request) {
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

func (s *Service) getBuiltInPolicyDefinition(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("policyDefinitionName")
	// El emulador no tiene definiciones BuiltIn reales.
	// Devolvemos 404 para que el provider azurerm sepa que no existe una
	// definición built-in con este nombre y proceda a crear la Custom.
	server.WriteError(w, http.StatusNotFound, "PolicyDefinitionNotFound",
		fmt.Sprintf("la definición built-in '%s' no existe en el emulador", name))
}
