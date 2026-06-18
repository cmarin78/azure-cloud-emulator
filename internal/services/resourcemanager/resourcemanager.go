// Package resourcemanager emula el subconjunto de Azure Resource Manager
// (ARM) que el resto de los servicios necesita como base: suscripciones
// "falsas" (se aceptan como válidas sin checks reales) y resource groups
// con su ciclo de vida completo (crear/actualizar, leer, listar, borrar).
//
// El borrado de un resource group en Azure real es asíncrono (puede
// cascadear sobre todos los recursos que contiene), así que aquí también
// se modela como una operación de larga duración (LRO) usando el helper
// de internal/server, igual que lo haría cualquier otro servicio futuro.
package resourcemanager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const resourceGroupsBucket = "resourcemanager.resourcegroups"

// Service agrupa el estado necesario para atender las rutas de Resource
// Manager: la base de datos embebida y el registro de operaciones async
// compartido con el resto del emulador.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de Resource Manager.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Subscription replica la forma mínima del recurso "subscription" de ARM.
// No hay validación real: cualquier GUID (o cualquier string) es aceptado,
// según lo definido en el ROADMAP para esta fase.
type Subscription struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscriptionId"`
	DisplayName    string `json:"displayName"`
	State          string `json:"state"`
}

// ResourceGroup replica la forma estándar de ARM para
// Microsoft.Resources/resourceGroups.
type ResourceGroup struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Location   string                  `json:"location"`
	Tags       map[string]string       `json:"tags,omitempty"`
	Properties ResourceGroupProperties `json:"properties"`
}

// ResourceGroupProperties contiene el único campo de "properties" que nos
// interesa emular: el estado de aprovisionamiento.
type ResourceGroupProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

type resourceGroupRequest struct {
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Register monta todas las rutas de Resource Manager en mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}", s.getSubscription)

	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups", s.listResourceGroups)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}", s.putResourceGroup)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}", s.getResourceGroup)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}", s.deleteResourceGroup)
}

// getSubscription "auto-vivifica" cualquier subscriptionId: no hace falta
// haberla creado antes, cualquier GUID (o string) es una suscripción
// válida y habilitada.
func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request) {
	subID := r.PathValue("subscriptionId")
	server.WriteJSON(w, http.StatusOK, Subscription{
		ID:             "/subscriptions/" + subID,
		SubscriptionID: subID,
		DisplayName:    "Emulated Subscription",
		State:          "Enabled",
	})
}

func resourceGroupKey(subID, name string) string {
	return subID + "/" + name
}

func (s *Service) putResourceGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("resourceGroupName")

	var req resourceGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio para crear o actualizar un resource group")
		return
	}

	key := resourceGroupKey(subID, name)
	var existing ResourceGroup
	found, err := s.db.Get(resourceGroupsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	rg := ResourceGroup{
		ID:       fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subID, name),
		Name:     name,
		Type:     "Microsoft.Resources/resourceGroups",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: ResourceGroupProperties{
			ProvisioningState: "Succeeded",
		},
	}

	if err := s.db.Put(resourceGroupsBucket, key, rg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rg)
}

func (s *Service) getResourceGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("resourceGroupName")

	var rg ResourceGroup
	found, err := s.db.Get(resourceGroupsBucket, resourceGroupKey(subID, name), &rg)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceGroupNotFound",
			fmt.Sprintf("el resource group '%s' no existe en la suscripción '%s'", name, subID))
		return
	}
	server.WriteJSON(w, http.StatusOK, rg)
}

func (s *Service) listResourceGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")

	groups := make([]ResourceGroup, 0)
	err := s.db.List(resourceGroupsBucket, subID+"/", func(key string, raw []byte) error {
		var rg ResourceGroup
		if err := json.Unmarshal(raw, &rg); err != nil {
			return err
		}
		groups = append(groups, rg)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": groups})
}

// deleteResourceGroup imita el comportamiento real de ARM: borrar un
// resource group que no existe es idempotente (204), y borrar uno que sí
// existe dispara una operación asíncrona (202 + Azure-AsyncOperation).
func (s *Service) deleteResourceGroup(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	name := r.PathValue("resourceGroupName")
	key := resourceGroupKey(subID, name)

	var existing ResourceGroup
	found, err := s.db.Get(resourceGroupsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.db.Delete(resourceGroupsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteAccepted(w, r, s.ops, subID, "Microsoft.Resources", "global", apiVersion)
}
