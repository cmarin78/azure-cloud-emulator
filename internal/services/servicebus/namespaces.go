// Package servicebus emula el subconjunto de Microsoft.ServiceBus que
// cubre el flujo más común de azurerm/az: namespaces (ARM, control
// plane), queues y topics/subscriptions (también ARM — a diferencia de
// Storage, en Azure real estas son sub-recursos ARM anidados bajo el
// namespace, no entidades de data-plane) y, por último, el data-plane real
// de envío/recepción de mensajes bajo el endpoint del namespace.
//
// Igual que storage accounts, crear un namespace es asíncrono en Azure
// real (aprovisiona infraestructura), así que el PUT responde 202 +
// Azure-AsyncOperation. Queues/topics/subscriptions, en cambio, son
// mutaciones rápidas y se modelan síncronas, igual que subnets/NICs en
// internal/services/network.
//
// El data-plane (enviar/recibir mensajes) vive en
// https://{namespace}.servicebus.windows.net/... en Azure real; aquí,
// siguiendo la misma convención path-style que blob/queue/table/keyvault,
// se sirve bajo "/{namespace}.servicebus/{resto-del-path}" a través del
// dispatcher compartido (ver cmd/azure-emulator/main.go).
package servicebus

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const namespacesBucket = "servicebus.namespaces"

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.ServiceBus (namespaces, queues, topics, subscriptions) y el
// data-plane de envío/recepción de mensajes.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de Service Bus.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Sku replica la forma mínima de "sku" que usa ARM para namespaces
// (p. ej. {"name": "Standard"}).
type Sku struct {
	Name string `json:"name"`
}

// Namespace replica la forma estándar de ARM para
// Microsoft.ServiceBus/namespaces.
type Namespace struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Sku        Sku                 `json:"sku"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties NamespaceProperties `json:"properties"`
}

// NamespaceProperties incluye el estado de aprovisionamiento y el
// endpoint del namespace, que es lo que az/Terraform leen después de
// crearlo para saber a dónde apuntar el data-plane.
type NamespaceProperties struct {
	ProvisioningState  string `json:"provisioningState"`
	ServiceBusEndpoint string `json:"serviceBusEndpoint"`
}

type namespaceRequest struct {
	Location string            `json:"location"`
	Sku      Sku               `json:"sku"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Register monta las rutas ARM de Microsoft.ServiceBus en mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces",
		s.listNamespaces)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}",
		s.putNamespace)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}",
		s.getNamespace)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}",
		s.deleteNamespace)

	s.registerQueues(mux)
	s.registerTopics(mux)
}

func namespaceKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func namespaceID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ServiceBus/namespaces/%s", subID, rg, name)
}

func namespaceEndpoint(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.servicebus/", scheme, r.Host, name)
}

func (s *Service) putNamespace(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")

	var req namespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio para crear un namespace")
		return
	}
	if strings.TrimSpace(req.Sku.Name) == "" {
		req.Sku.Name = "Standard"
	}

	key := namespaceKey(subID, rg, name)
	ns := Namespace{
		ID:       namespaceID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.ServiceBus/namespaces",
		Location: req.Location,
		Sku:      req.Sku,
		Tags:     req.Tags,
		Properties: NamespaceProperties{
			ProvisioningState:  "Succeeded",
			ServiceBusEndpoint: namespaceEndpoint(r, name),
		},
	}

	if err := s.db.Put(namespacesBucket, key, ns); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Igual que storage accounts: crear un namespace es asíncrono en
	// Azure real, así que devolvemos 202 + Azure-AsyncOperation aunque el
	// emulador ya deje el recurso en "Succeeded" de inmediato.
	w.Header().Set("Content-Type", "application/json")
	id := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, "Microsoft.ServiceBus", req.Location, id, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, ns)
}

func (s *Service) getNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")

	ns, found, err := s.getNS(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el namespace '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, ns)
}

func (s *Service) listNamespaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	namespaces := make([]Namespace, 0)
	err := s.db.List(namespacesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var ns Namespace
		if err := json.Unmarshal(raw, &ns); err != nil {
			return err
		}
		namespaces = append(namespaces, ns)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": namespaces})
}

// deleteNamespace es idempotente (204 si no existe) y síncrono en este
// emulador (en Azure real el borrado también es asíncrono, pero igual que
// storage accounts simplificamos: az/Terraform solo necesitan que vuelva
// cuando el namespace ya no exista). No borra en cascada queues/topics
// (igual de simplificado que virtualNetworks con sus NICs).
func (s *Service) deleteNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")
	key := namespaceKey(subID, rg, name)

	found, err := s.db.Get(namespacesBucket, key, &Namespace{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(namespacesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getNS es el helper interno (usado también por queues.go/topics.go para
// validar que el namespace padre existe) que busca un namespace por
// subID/rg/name.
func (s *Service) getNS(subID, rg, name string) (Namespace, bool, error) {
	var ns Namespace
	found, err := s.db.Get(namespacesBucket, namespaceKey(subID, rg, name), &ns)
	return ns, found, err
}
