package managedidentity

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const identitiesBucket = "managedidentity.userassignedidentities"

// UserAssignedIdentity replica el subconjunto relevante de
// Microsoft.ManagedIdentity/userAssignedIdentities que az/Terraform
// (azurerm_user_assigned_identity) leen. A diferencia de la mayoría de los
// recursos de este proyecto, Azure real no expone "properties.provisioningState"
// aquí -- el shape de properties es solo tenantId/principalId/clientId.
type UserAssignedIdentity struct {
	ID         string                         `json:"id"`
	Name       string                         `json:"name"`
	Type       string                         `json:"type"`
	Location   string                         `json:"location"`
	Tags       map[string]string              `json:"tags,omitempty"`
	Properties UserAssignedIdentityProperties `json:"properties"`
}

type UserAssignedIdentityProperties struct {
	TenantID    string `json:"tenantId"`
	PrincipalID string `json:"principalId"`
	ClientID    string `json:"clientId"`
}

type identityRequest struct {
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags,omitempty"`
}

func identityKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func identityID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ManagedIdentity/userAssignedIdentities/%s", subID, rg, name)
}

// fakeHexSuffix/fakeGUID derivan valores deterministas a partir del ID
// completo del recurso, mismo patrón que aks.fakeHexSuffix/fakeGUID
// (Phase 13), compute.fakeHexSuffix/fakeGUID y appservice.fakeHexSuffix/
// fakeGUID (ambos Phase 16) -- cada paquete mantiene su propia copia local
// en vez de un helper compartido.
func fakeHexSuffix(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

func fakeGUID(seed string) string {
	sum := fakeHexSuffix(seed)
	return fmt.Sprintf("%08x-0000-0000-0000-%012x", sum, uint64(sum)*2654435761)
}

func (s *Service) registerUserAssignedIdentities(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ManagedIdentity/userAssignedIdentities"
	mux.HandleFunc("GET "+base, s.listUserAssignedIdentities)
	mux.HandleFunc("PUT "+base+"/{identityName}", s.putUserAssignedIdentity)
	mux.HandleFunc("GET "+base+"/{identityName}", s.getUserAssignedIdentityHandler)
	mux.HandleFunc("DELETE "+base+"/{identityName}", s.deleteUserAssignedIdentity)
}

// putUserAssignedIdentity es síncrono (Effort "S", igual que vaults/disks/
// App Service Plans): crear una identidad en Azure real no requiere polling
// en los flujos comunes de az CLI/Terraform. tenantId/principalId/clientId
// se derivan de forma determinista del ID del recurso la primera vez que se
// crea, y se preservan en updates posteriores (igual que numberOfSites en
// appservice.AppServicePlan) para que no "roten" en cada PUT idempotente.
func (s *Service) putUserAssignedIdentity(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("identityName")

	var req identityRequest
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

	key := identityKey(subID, rg, name)
	existing, found, err := s.getIdentity(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	id := identityID(subID, rg, name)
	props := UserAssignedIdentityProperties{
		TenantID:    fakeGUID(id + "-tenant"),
		PrincipalID: fakeGUID(id + "-principal"),
		ClientID:    fakeGUID(id + "-client"),
	}
	if found {
		props = existing.Properties
	}

	identity := UserAssignedIdentity{
		ID:         id,
		Name:       name,
		Type:       "Microsoft.ManagedIdentity/userAssignedIdentities",
		Location:   req.Location,
		Tags:       req.Tags,
		Properties: props,
	}

	if err := s.db.Put(identitiesBucket, key, identity); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, identity)
}

func (s *Service) getIdentity(subID, rg, name string) (UserAssignedIdentity, bool, error) {
	var identity UserAssignedIdentity
	found, err := s.db.Get(identitiesBucket, identityKey(subID, rg, name), &identity)
	return identity, found, err
}

func (s *Service) getUserAssignedIdentityHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("identityName")

	identity, found, err := s.getIdentity(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la managed identity '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, identity)
}

func (s *Service) listUserAssignedIdentities(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	identities := make([]UserAssignedIdentity, 0)
	err := s.db.List(identitiesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var identity UserAssignedIdentity
		if err := json.Unmarshal(raw, &identity); err != nil {
			return err
		}
		identities = append(identities, identity)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": identities})
}

// deleteUserAssignedIdentity es idempotente (204 si no existe) y síncrono,
// igual que deletePlan/deleteVault. No valida que ningún otro recurso la
// siga referenciando en su bloque identity.userAssignedIdentities (mismo
// enfoque "sin integridad referencial estricta" que metricAlerts.actionGroupId).
func (s *Service) deleteUserAssignedIdentity(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("identityName")
	key := identityKey(subID, rg, name)

	found, err := s.db.Get(identitiesBucket, key, &UserAssignedIdentity{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(identitiesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
