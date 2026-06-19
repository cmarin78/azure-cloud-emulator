package servicebus

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const queuesBucket = "servicebus.queues"

// Queue replica la forma estándar de ARM para
// Microsoft.ServiceBus/namespaces/queues. A diferencia de Storage Queue
// (data-plane), en Service Bus las colas son un sub-recurso ARM anidado
// bajo el namespace — así es como azurerm_servicebus_queue/az servicebus
// queue las gestionan en la vida real.
type Queue struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Properties QueueProperties `json:"properties"`
}

type QueueProperties struct {
	ProvisioningState  string `json:"provisioningState"`
	MaxSizeInMegabytes int    `json:"maxSizeInMegabytes,omitempty"`
}

type queueRequest struct {
	Properties struct {
		MaxSizeInMegabytes int `json:"maxSizeInMegabytes,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerQueues(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/queues",
		s.listQueues)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/queues/{queueName}",
		s.putQueue)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/queues/{queueName}",
		s.getQueue)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/queues/{queueName}",
		s.deleteQueue)
}

func queueKey(subID, rg, ns, queue string) string {
	return subID + "/" + rg + "/" + ns + "/" + queue
}

func (s *Service) putQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	queueName := r.PathValue("queueName")

	ns, found, err := s.getNS(subID, rg, nsName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el namespace '%s' no existe en el resource group '%s'", nsName, rg))
		return
	}

	var req queueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	key := queueKey(subID, rg, nsName, queueName)
	_, existedBefore, err := s.getQueueRecord(subID, rg, nsName, queueName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	q := Queue{
		ID:   ns.ID + "/queues/" + queueName,
		Name: queueName,
		Type: "Microsoft.ServiceBus/namespaces/queues",
		Properties: QueueProperties{
			ProvisioningState:  "Succeeded",
			MaxSizeInMegabytes: req.Properties.MaxSizeInMegabytes,
		},
	}
	if err := s.db.Put(queuesBucket, key, q); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneEntity(queueEntityPath(nsName, queueName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, q)
}

func (s *Service) getQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	queueName := r.PathValue("queueName")

	q, found, err := s.getQueueRecord(subID, rg, nsName, queueName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la cola '%s' no existe en el namespace '%s'", queueName, nsName))
		return
	}
	server.WriteJSON(w, http.StatusOK, q)
}

func (s *Service) listQueues(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")

	queues := make([]Queue, 0)
	err := s.db.List(queuesBucket, subID+"/"+rg+"/"+nsName+"/", func(key string, raw []byte) error {
		var q Queue
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		queues = append(queues, q)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": queues})
}

// deleteQueue es idempotente (204 si no existe). No borra en cascada los
// mensajes encolados vía data-plane (igual de simplificado que el resto
// del emulador con recursos padre/hijo).
func (s *Service) deleteQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	queueName := r.PathValue("queueName")
	key := queueKey(subID, rg, nsName, queueName)

	found, err := s.db.Get(queuesBucket, key, &Queue{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(queuesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.unmarkDataPlaneEntity(queueEntityPath(nsName, queueName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// getQueueRecord es el helper interno que busca una cola ARM por
// subID/rg/ns/name.
func (s *Service) getQueueRecord(subID, rg, ns, name string) (Queue, bool, error) {
	var q Queue
	found, err := s.db.Get(queuesBucket, queueKey(subID, rg, ns, name), &q)
	return q, found, err
}
