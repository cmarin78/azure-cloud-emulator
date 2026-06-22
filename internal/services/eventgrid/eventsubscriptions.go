package eventgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const eventSubscriptionsBucket = "eventgrid.eventsubscriptions"

// EventSubscription replica el subconjunto relevante de
// Microsoft.EventGrid/eventSubscriptions necesario para que
// azurerm_eventgrid_event_subscription (con scope = ID de un topic) opere
// extremo a extremo. En Azure real esto es un "extension resource": su ARM
// ID anida un segundo /providers/Microsoft.EventGrid/ después del topic
// (ver registerEventSubscriptions), a diferencia de queues/topics de
// Service Bus que son sub-recursos "normales" de un solo nivel.
type EventSubscription struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Type       string                      `json:"type"`
	Properties EventSubscriptionProperties `json:"properties"`
}

// EventSubscriptionProperties solo modela destination (y, dentro de ella,
// el sub-caso WebHook que sí se despacha de verdad — Fase 17). filter se
// guarda tal cual como json.RawMessage porque nunca se evalúa (ver
// eventgrid.go: "no hay evaluación de filtros").
type EventSubscriptionProperties struct {
	ProvisioningState   string          `json:"provisioningState"`
	Destination         json.RawMessage `json:"destination"`
	Filter              json.RawMessage `json:"filter,omitempty"`
	EventDeliverySchema string          `json:"eventDeliverySchema,omitempty"`

	// LastDeliveryTime/LastDeliveryStatus son una extensión del emulador
	// (no existen en el shape real de Azure) para observar el resultado del
	// último despacho de webhook sin necesitar un sink de activity log
	// propio — mismo enfoque que monitor.ActionGroupProperties.
	// LastNotificationTime/LastNotificationStatus de la Fase 20. Los
	// clientes reales ignoran campos JSON desconocidos en la respuesta.
	LastDeliveryTime   string `json:"lastDeliveryTime,omitempty"`
	LastDeliveryStatus string `json:"lastDeliveryStatus,omitempty"`
}

// webhookDestination replica el subconjunto de
// WebHookEventSubscriptionDestination necesario para despachar de verdad.
type webhookDestination struct {
	EndpointType string `json:"endpointType"`
	Properties   struct {
		EndpointURL string `json:"endpointUrl"`
	} `json:"properties"`
}

type eventSubscriptionRequest struct {
	Properties struct {
		Destination         json.RawMessage `json:"destination"`
		Filter              json.RawMessage `json:"filter"`
		EventDeliverySchema string          `json:"eventDeliverySchema"`
	} `json:"properties"`
}

// registerEventSubscriptions monta las rutas de eventSubscriptions
// ancladas bajo un topic. El shape real de Azure repite
// "/providers/Microsoft.EventGrid/" una segunda vez porque
// eventSubscriptions es un extension resource sobre el scope del topic, no
// un sub-recurso normal -- a diferencia de
// Microsoft.ServiceBus/namespaces/topics/subscriptions (un solo
// /providers/ en toda la ruta). No es un patrón de profundidad variable
// (a diferencia de diagnosticSettings, fuera de alcance según
// monitor.go): aquí el scope siempre es exactamente un topic, así que la
// ruta sigue siendo literal de extremo a extremo.
func (s *Service) registerEventSubscriptions(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.EventGrid/topics/{topicName}/providers/Microsoft.EventGrid/eventSubscriptions"
	mux.HandleFunc("GET "+base, s.listEventSubscriptions)
	mux.HandleFunc("PUT "+base+"/{eventSubscriptionName}", s.putEventSubscription)
	mux.HandleFunc("GET "+base+"/{eventSubscriptionName}", s.getEventSubscription)
	mux.HandleFunc("DELETE "+base+"/{eventSubscriptionName}", s.deleteEventSubscription)
}

func eventSubscriptionKey(subID, rg, topic, name string) string {
	return subID + "/" + rg + "/" + topic + "/" + name
}

func (s *Service) putEventSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	topicName := r.PathValue("topicName")
	name := r.PathValue("eventSubscriptionName")

	topic, found, err := s.getTopicRecord(subID, rg, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el topic '%s' no existe en el resource group '%s'", topicName, rg))
		return
	}

	var req eventSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if len(req.Properties.Destination) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.destination' es obligatorio")
		return
	}

	key := eventSubscriptionKey(subID, rg, topicName, name)
	_, existedBefore, err := s.getEventSubscriptionRecord(subID, rg, topicName, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	es := EventSubscription{
		ID:   topic.ID + "/providers/Microsoft.EventGrid/eventSubscriptions/" + name,
		Name: name,
		Type: "Microsoft.EventGrid/eventSubscriptions",
		Properties: EventSubscriptionProperties{
			ProvisioningState:   "Succeeded",
			Destination:         req.Properties.Destination,
			Filter:              req.Properties.Filter,
			EventDeliverySchema: req.Properties.EventDeliverySchema,
		},
	}
	if err := s.db.Put(eventSubscriptionsBucket, key, es); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, es)
}

func (s *Service) getEventSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	topicName := r.PathValue("topicName")
	name := r.PathValue("eventSubscriptionName")

	es, found, err := s.getEventSubscriptionRecord(subID, rg, topicName, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la event subscription '%s' no existe en el topic '%s'", name, topicName))
		return
	}
	server.WriteJSON(w, http.StatusOK, es)
}

func (s *Service) listEventSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	topicName := r.PathValue("topicName")

	list, err := s.listEventSubscriptionRecords(subID, rg, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": list})
}

func (s *Service) deleteEventSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	topicName := r.PathValue("topicName")
	name := r.PathValue("eventSubscriptionName")
	key := eventSubscriptionKey(subID, rg, topicName, name)

	found, err := s.db.Get(eventSubscriptionsBucket, key, &EventSubscription{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(eventSubscriptionsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getEventSubscriptionRecord(subID, rg, topic, name string) (EventSubscription, bool, error) {
	var es EventSubscription
	found, err := s.db.Get(eventSubscriptionsBucket, eventSubscriptionKey(subID, rg, topic, name), &es)
	return es, found, err
}

func (s *Service) listEventSubscriptionRecords(subID, rg, topic string) ([]EventSubscription, error) {
	list := make([]EventSubscription, 0)
	err := s.db.List(eventSubscriptionsBucket, subID+"/"+rg+"/"+topic+"/", func(_ string, raw []byte) error {
		var es EventSubscription
		if err := json.Unmarshal(raw, &es); err != nil {
			return err
		}
		list = append(list, es)
		return nil
	})
	return list, err
}

// dispatchToSubscription envía un POST HTTP real con el body publicado
// (el array de eventos, tal cual) al endpoint webhook de una event
// subscription, y registra el resultado (LastDeliveryTime/
// LastDeliveryStatus) sobre el registro persistido -- mismo patrón que
// monitor.Service.dispatch para Action Groups, salvo que aquí se invoca
// desde una goroutine separada por subscription (ver dataplane.go:
// publish es la ruta caliente de un pipeline de eventos, no una acción
// administrativa puntual como createNotifications).
func (s *Service) dispatchToSubscription(subID, rg, topic, name string, body []byte) {
	es, found, err := s.getEventSubscriptionRecord(subID, rg, topic, name)
	if err != nil || !found {
		return
	}

	var dest webhookDestination
	if err := json.Unmarshal(es.Properties.Destination, &dest); err != nil || dest.EndpointType != "WebHook" {
		return
	}
	if strings.TrimSpace(dest.Properties.EndpointURL) == "" {
		return
	}

	status := "ok"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.Properties.EndpointURL, bytes.NewReader(body))
	if err != nil {
		status = err.Error()
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Aeg-Event-Type", "Notification")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			status = err.Error()
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				status = fmt.Sprintf("http %d", resp.StatusCode)
			}
		}
	}
	cancel()

	es.Properties.LastDeliveryTime = time.Now().UTC().Format(time.RFC3339)
	es.Properties.LastDeliveryStatus = status
	_ = s.db.Put(eventSubscriptionsBucket, eventSubscriptionKey(subID, rg, topic, name), es)
}
