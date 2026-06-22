package eventgrid

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const topicsBucket = "eventgrid.topics"

// Topic replica la forma estándar de ARM para Microsoft.EventGrid/topics.
type Topic struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties TopicProperties   `json:"properties"`
}

// TopicProperties incluye el endpoint de publish, que es lo que
// az/Terraform leen después de crear el topic para saber a dónde apuntar
// el data-plane (igual que servicebus.NamespaceProperties.ServiceBusEndpoint).
type TopicProperties struct {
	ProvisioningState string `json:"provisioningState"`
	Endpoint          string `json:"endpoint"`
	InputSchema       string `json:"inputSchema"`
}

type topicRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		InputSchema string `json:"inputSchema"`
	} `json:"properties"`
}

func (s *Service) registerTopics(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.EventGrid/topics"
	mux.HandleFunc("GET "+base, s.listTopics)
	mux.HandleFunc("PUT "+base+"/{topicName}", s.putTopic)
	mux.HandleFunc("GET "+base+"/{topicName}", s.getTopic)
	mux.HandleFunc("DELETE "+base+"/{topicName}", s.deleteTopic)
}

func topicKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func topicID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.EventGrid/topics/%s", subID, rg, name)
}

// topicEndpoint deriva el endpoint de publish del topic a partir del host
// de la request, igual que servicebus.namespaceEndpoint. Azure real usa
// "https://{topic}.{region}-1.eventgrid.azure.net/api/events"; aquí, con la
// misma convención path-style que el resto de servicios data-plane, se
// sirve bajo "/{topic}.eventgrid/api/events" a través del dispatcher
// compartido (ver cmd/azure-emulator/main.go).
func topicEndpoint(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s.eventgrid/api/events", scheme, r.Host, name)
}

func (s *Service) putTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("topicName")

	var req topicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio para crear un topic")
		return
	}
	inputSchema := req.Properties.InputSchema
	if strings.TrimSpace(inputSchema) == "" {
		inputSchema = "EventGridSchema"
	}

	key := topicKey(subID, rg, name)
	_, existedBefore, err := s.getTopicRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	t := Topic{
		ID:       topicID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.EventGrid/topics",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: TopicProperties{
			ProvisioningState: "Succeeded",
			Endpoint:          topicEndpoint(r, name),
			InputSchema:       inputSchema,
		},
	}
	if err := s.db.Put(topicsBucket, key, t); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneTopic(name, subID, rg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, t)
}

func (s *Service) getTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("topicName")

	t, found, err := s.getTopicRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el topic '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, t)
}

func (s *Service) listTopics(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	topics := make([]Topic, 0)
	err := s.db.List(topicsBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
		var t Topic
		if err := json.Unmarshal(raw, &t); err != nil {
			return err
		}
		topics = append(topics, t)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": topics})
}

// deleteTopic es idempotente (204 si no existe). No borra en cascada sus
// event subscriptions (igual de simplificado que servicebus.deleteNamespace
// con sus queues/topics).
func (s *Service) deleteTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("topicName")
	key := topicKey(subID, rg, name)

	found, err := s.db.Get(topicsBucket, key, &Topic{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(topicsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.unmarkDataPlaneTopic(name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getTopicRecord(subID, rg, name string) (Topic, bool, error) {
	var t Topic
	found, err := s.db.Get(topicsBucket, topicKey(subID, rg, name), &t)
	return t, found, err
}
