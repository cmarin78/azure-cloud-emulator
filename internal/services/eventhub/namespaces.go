package eventhub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const namespacesBucket = "eventhub.namespaces"

// Sku replica la forma mínima de "sku" que usa ARM para namespaces (p.
// ej. {"name": "Standard"}).
type Sku struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity,omitempty"`
}

// Namespace replica la forma estándar de ARM para
// Microsoft.EventHub/namespaces.
type Namespace struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Sku        Sku                 `json:"sku"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties NamespaceProperties `json:"properties"`
}

type NamespaceProperties struct {
	ProvisioningState  string `json:"provisioningState"`
	ServiceBusEndpoint string `json:"serviceBusEndpoint"`
}

type namespaceRequest struct {
	Location string            `json:"location"`
	Sku      Sku               `json:"sku"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Register monta las rutas ARM de Microsoft.EventHub/namespaces en mux.
func (s *Service) registerNamespaces(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.EventHub/namespaces"
	mux.HandleFunc("GET "+base, s.listNamespaces)
	mux.HandleFunc("PUT "+base+"/{namespaceName}", s.putNamespace)
	mux.HandleFunc("GET "+base+"/{namespaceName}", s.getNamespace)
	mux.HandleFunc("DELETE "+base+"/{namespaceName}", s.deleteNamespace)
}

func namespaceKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func namespaceID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.EventHub/namespaces/%s", subID, rg, name)
}

// namespaceEndpoint replica servicebus.namespaceEndpoint salvo el campo
// JSON, que Azure real nombra igual ("serviceBusEndpoint") porque Event
// Hubs comparte el protocolo AMQP de Service Bus -- pero el shape
// path-style de este emulador usa el sufijo ".eventhub" en vez de
// ".servicebus" para no colisionar en el dispatcher compartido (ver
// eventhub.go).
func namespaceEndpoint(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.eventhub/", scheme, r.Host, name)
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
		Type:     "Microsoft.EventHub/namespaces",
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

	// Igual que servicebus.putNamespace: crear un namespace de Event Hubs
	// es asíncrono en Azure real, así que devolvemos 202 +
	// Azure-AsyncOperation aunque el emulador ya deje el recurso en
	// "Succeeded" de inmediato.
	w.Header().Set("Content-Type", "application/json")
	id := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, "Microsoft.EventHub", req.Location, id, apiVersion)
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
	err := s.db.List(namespacesBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
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

// deleteNamespace es idempotente (204 si no existe) y no borra en cascada
// sus event hubs/consumer groups -- mismo enfoque simplificado que
// servicebus.deleteNamespace.
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

// getNS es el helper interno (usado también por hubs.go para validar que
// el namespace padre existe) que busca un namespace por subID/rg/name.
func (s *Service) getNS(subID, rg, name string) (Namespace, bool, error) {
	var ns Namespace
	found, err := s.db.Get(namespacesBucket, namespaceKey(subID, rg, name), &ns)
	return ns, found, err
}
