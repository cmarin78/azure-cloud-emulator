package authorization

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// Dos buckets separados (en vez de uno compartido con un prefijo de scope
// en la key) para que listar a nivel de suscripción nunca incluya
// asignaciones de un resource group cuyo subscriptionId coincide -- un
// prefijo de key compartido tipo "{subId}/" colisionaría con
// "{subId}/resourceGroups/{rg}/{name}", que también empieza por "{subId}/".
const (
	roleAssignmentsSubBucket = "authorization.roleassignments.subscription"
	roleAssignmentsRGBucket  = "authorization.roleassignments.resourcegroup"
)

// RoleAssignment replica el subconjunto relevante de
// Microsoft.Authorization/roleAssignments. No se valida que roleDefinitionId
// ni principalId existan realmente -- mismo enfoque de "no integridad
// referencial" que el resto del proyecto.
type RoleAssignment struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Properties RoleAssignmentProperties `json:"properties"`
}

type RoleAssignmentProperties struct {
	Scope            string `json:"scope"`
	RoleDefinitionID string `json:"roleDefinitionId"`
	PrincipalID      string `json:"principalId"`
	PrincipalType    string `json:"principalType,omitempty"`
}

type roleAssignmentRequest struct {
	Properties struct {
		RoleDefinitionID string `json:"roleDefinitionId"`
		PrincipalID      string `json:"principalId"`
		PrincipalType    string `json:"principalType,omitempty"`
	} `json:"properties"`
}

func roleAssignmentARMID(scope, name string) string {
	return scope + "/providers/Microsoft.Authorization/roleAssignments/" + name
}

// newRoleAssignmentName genera un GUID aleatorio para el nombre de la
// asignación cuando el cliente no especifica uno explícito vía
// "roleAssignmentName" en el cuerpo -- az CLI/azurerm normalmente generan
// este GUID ellos mismos y lo pasan en la URL, así que en la práctica este
// fallback rara vez se usa, pero lo dejamos por completitud.
func newRoleAssignmentName() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("authorization: error generando id aleatorio: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}

func (s *Service) registerRoleAssignments(mux *http.ServeMux) {
	subBase := "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleAssignments"
	mux.HandleFunc("GET "+subBase, s.listRoleAssignmentsSub)
	mux.HandleFunc("PUT "+subBase+"/{roleAssignmentName}", s.putRoleAssignmentSub)
	mux.HandleFunc("GET "+subBase+"/{roleAssignmentName}", s.getRoleAssignmentSub)
	mux.HandleFunc("DELETE "+subBase+"/{roleAssignmentName}", s.deleteRoleAssignmentSub)

	rgBase := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Authorization/roleAssignments"
	mux.HandleFunc("GET "+rgBase, s.listRoleAssignmentsRG)
	mux.HandleFunc("PUT "+rgBase+"/{roleAssignmentName}", s.putRoleAssignmentRG)
	mux.HandleFunc("GET "+rgBase+"/{roleAssignmentName}", s.getRoleAssignmentRG)
	mux.HandleFunc("DELETE "+rgBase+"/{roleAssignmentName}", s.deleteRoleAssignmentRG)
}

// --- scope: suscripción --------------------------------------------------

func (s *Service) putRoleAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	scope := "/subscriptions/" + subID
	s.putRoleAssignment(w, r, roleAssignmentsSubBucket, scope, subID)
}

func (s *Service) getRoleAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	s.getRoleAssignment(w, r, roleAssignmentsSubBucket, subID)
}

func (s *Service) deleteRoleAssignmentSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	s.deleteRoleAssignment(w, r, roleAssignmentsSubBucket, subID)
}

func (s *Service) listRoleAssignmentsSub(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	s.listRoleAssignments(w, r, roleAssignmentsSubBucket, subID+"/")
}

// --- scope: resource group ------------------------------------------------

func (s *Service) putRoleAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subID, rg)
	s.putRoleAssignment(w, r, roleAssignmentsRGBucket, scope, subID+"/"+rg)
}

func (s *Service) getRoleAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	s.getRoleAssignment(w, r, roleAssignmentsRGBucket, subID+"/"+rg)
}

func (s *Service) deleteRoleAssignmentRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	s.deleteRoleAssignment(w, r, roleAssignmentsRGBucket, subID+"/"+rg)
}

func (s *Service) listRoleAssignmentsRG(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	s.listRoleAssignments(w, r, roleAssignmentsRGBucket, subID+"/"+rg+"/")
}

// --- lógica compartida ----------------------------------------------------

func (s *Service) putRoleAssignment(w http.ResponseWriter, r *http.Request, bucket, scope, keyPrefix string) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	name := r.PathValue("roleAssignmentName")

	var req roleAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.RoleDefinitionID) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.roleDefinitionId' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.PrincipalID) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.principalId' es obligatorio")
		return
	}

	if name == "" {
		generated, err := newRoleAssignmentName()
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		name = generated
	}

	key := keyPrefix + "/" + name
	_, found, err := s.getRoleAssignmentByKey(bucket, key)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	ra := RoleAssignment{
		ID:   roleAssignmentARMID(scope, name),
		Name: name,
		Type: "Microsoft.Authorization/roleAssignments",
		Properties: RoleAssignmentProperties{
			Scope:            scope,
			RoleDefinitionID: req.Properties.RoleDefinitionID,
			PrincipalID:      req.Properties.PrincipalID,
			PrincipalType:    req.Properties.PrincipalType,
		},
	}

	if err := s.db.Put(bucket, key, ra); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, ra)
}

func (s *Service) getRoleAssignmentByKey(bucket, key string) (RoleAssignment, bool, error) {
	var ra RoleAssignment
	found, err := s.db.Get(bucket, key, &ra)
	return ra, found, err
}

func (s *Service) getRoleAssignment(w http.ResponseWriter, r *http.Request, bucket, keyPrefix string) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	name := r.PathValue("roleAssignmentName")
	key := keyPrefix + "/" + name

	ra, found, err := s.getRoleAssignmentByKey(bucket, key)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "RoleAssignmentNotFound",
			fmt.Sprintf("la asignación de rol '%s' no existe", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, ra)
}

func (s *Service) listRoleAssignments(w http.ResponseWriter, r *http.Request, bucket, listPrefix string) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	assignments := make([]RoleAssignment, 0)
	err := s.db.List(bucket, listPrefix, func(key string, raw []byte) error {
		var ra RoleAssignment
		if err := json.Unmarshal(raw, &ra); err != nil {
			return err
		}
		assignments = append(assignments, ra)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": assignments})
}

// deleteRoleAssignment es idempotente (204 si no existe), mismo patrón que
// el resto del proyecto.
func (s *Service) deleteRoleAssignment(w http.ResponseWriter, r *http.Request, bucket, keyPrefix string) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	name := r.PathValue("roleAssignmentName")
	key := keyPrefix + "/" + name

	found, err := s.db.Get(bucket, key, &RoleAssignment{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(bucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
