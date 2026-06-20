package authorization

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const roleDefinitionsBucket = "authorization.roledefinitions"

// RoleDefinition replica el subconjunto relevante de
// Microsoft.Authorization/roleDefinitions. assignableScopes y permissions
// se persisten tal cual sin validarse contra el resto del emulador -- mismo
// enfoque de "no integridad referencial" que el resto del proyecto.
type RoleDefinition struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Properties RoleDefinitionProperties `json:"properties"`
}

type RoleDefinitionProperties struct {
	RoleName         string                     `json:"roleName"`
	Description      string                     `json:"description,omitempty"`
	RoleType         string                     `json:"type"`
	AssignableScopes []string                   `json:"assignableScopes"`
	Permissions      []RoleDefinitionPermission `json:"permissions"`
}

type RoleDefinitionPermission struct {
	Actions        []string `json:"actions,omitempty"`
	NotActions     []string `json:"notActions,omitempty"`
	DataActions    []string `json:"dataActions,omitempty"`
	NotDataActions []string `json:"notDataActions,omitempty"`
}

type roleDefinitionRequest struct {
	Properties struct {
		RoleName         string                     `json:"roleName"`
		Description      string                     `json:"description,omitempty"`
		AssignableScopes []string                   `json:"assignableScopes"`
		Permissions      []RoleDefinitionPermission `json:"permissions"`
	} `json:"properties"`
}

func roleDefinitionKey(subID, roleDefinitionID string) string {
	return subID + "/" + roleDefinitionID
}

func roleDefinitionARMID(subID, roleDefinitionID string) string {
	return fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subID, roleDefinitionID)
}

func (s *Service) registerRoleDefinitions(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleDefinitions"
	mux.HandleFunc("GET "+base, s.listRoleDefinitions)
	mux.HandleFunc("PUT "+base+"/{roleDefinitionId}", s.putRoleDefinition)
	mux.HandleFunc("GET "+base+"/{roleDefinitionId}", s.getRoleDefinitionHandler)
	mux.HandleFunc("DELETE "+base+"/{roleDefinitionId}", s.deleteRoleDefinition)
}

func (s *Service) putRoleDefinition(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	roleDefinitionID := r.PathValue("roleDefinitionId")

	var req roleDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.RoleName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.roleName' es obligatorio")
		return
	}
	if len(req.Properties.AssignableScopes) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.assignableScopes' es obligatorio (azurerm_role_definition siempre lo envía)")
		return
	}

	key := roleDefinitionKey(subID, roleDefinitionID)
	_, found, err := s.getRoleDefinition(subID, roleDefinitionID)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	rd := RoleDefinition{
		ID:   roleDefinitionARMID(subID, roleDefinitionID),
		Name: roleDefinitionID,
		Type: "Microsoft.Authorization/roleDefinitions",
		Properties: RoleDefinitionProperties{
			RoleName:         req.Properties.RoleName,
			Description:      req.Properties.Description,
			RoleType:         "CustomRole",
			AssignableScopes: req.Properties.AssignableScopes,
			Permissions:      req.Properties.Permissions,
		},
	}

	if err := s.db.Put(roleDefinitionsBucket, key, rd); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rd)
}

func (s *Service) getRoleDefinition(subID, roleDefinitionID string) (RoleDefinition, bool, error) {
	var rd RoleDefinition
	found, err := s.db.Get(roleDefinitionsBucket, roleDefinitionKey(subID, roleDefinitionID), &rd)
	return rd, found, err
}

func (s *Service) getRoleDefinitionHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	roleDefinitionID := r.PathValue("roleDefinitionId")

	rd, found, err := s.getRoleDefinition(subID, roleDefinitionID)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "RoleDefinitionDoesNotExist",
			fmt.Sprintf("la definición de rol '%s' no existe en la suscripción '%s'", roleDefinitionID, subID))
		return
	}
	server.WriteJSON(w, http.StatusOK, rd)
}

func (s *Service) listRoleDefinitions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")

	defs := make([]RoleDefinition, 0)
	err := s.db.List(roleDefinitionsBucket, subID+"/", func(key string, raw []byte) error {
		var rd RoleDefinition
		if err := json.Unmarshal(raw, &rd); err != nil {
			return err
		}
		defs = append(defs, rd)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": defs})
}

// deleteRoleDefinition es idempotente (204 si no existe), mismo patrón que
// el resto del proyecto.
func (s *Service) deleteRoleDefinition(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	roleDefinitionID := r.PathValue("roleDefinitionId")
	key := roleDefinitionKey(subID, roleDefinitionID)

	found, err := s.db.Get(roleDefinitionsBucket, key, &RoleDefinition{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(roleDefinitionsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
