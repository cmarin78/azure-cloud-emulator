package eventhub

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const hubsBucket = "eventhub.hubs"
const consumerGroupsBucket = "eventhub.consumergroups"

// EventHubResource replica la forma estándar de ARM para
// Microsoft.EventHub/namespaces/eventhubs. Se llama "EventHubResource" (no
// "EventHub") para no chocar con el nombre del paquete.
type EventHubResource struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Properties EventHubProperties `json:"properties"`
}

type EventHubProperties struct {
	ProvisioningState string `json:"provisioningState"`
	PartitionCount    int    `json:"partitionCount,omitempty"`
	// PartitionIds se rellena con IDs "0".."partitionCount-1" como string,
	// igual que devuelve Azure real -- aunque el data-plane simplificado
	// (ver dataplane.go) no reparte eventos entre particiones de verdad.
	PartitionIds []string `json:"partitionIds,omitempty"`
}

// ConsumerGroup replica la forma estándar de ARM para
// Microsoft.EventHub/namespaces/eventhubs/consumergroups.
type ConsumerGroup struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Properties ConsumerGroupProperties `json:"properties"`
}

type ConsumerGroupProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

type hubRequest struct {
	Properties struct {
		PartitionCount int `json:"partitionCount"`
	} `json:"properties"`
}

func (s *Service) registerHubs(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.EventHub/namespaces/{namespaceName}/eventhubs"
	mux.HandleFunc("GET "+base, s.listHubs)
	mux.HandleFunc("PUT "+base+"/{eventHubName}", s.putHub)
	mux.HandleFunc("GET "+base+"/{eventHubName}", s.getHub)
	mux.HandleFunc("DELETE "+base+"/{eventHubName}", s.deleteHub)

	cgBase := base + "/{eventHubName}/consumergroups"
	mux.HandleFunc("GET "+cgBase, s.listConsumerGroups)
	mux.HandleFunc("PUT "+cgBase+"/{consumerGroupName}", s.putConsumerGroup)
	mux.HandleFunc("GET "+cgBase+"/{consumerGroupName}", s.getConsumerGroup)
	mux.HandleFunc("DELETE "+cgBase+"/{consumerGroupName}", s.deleteConsumerGroup)
}

func hubKey(subID, rg, ns, hub string) string {
	return subID + "/" + rg + "/" + ns + "/" + hub
}

func consumerGroupKey(subID, rg, ns, hub, cg string) string {
	return subID + "/" + rg + "/" + ns + "/" + hub + "/" + cg
}

func (s *Service) putHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")

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

	var req hubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	partitionCount := req.Properties.PartitionCount
	if partitionCount <= 0 {
		partitionCount = 4
	}
	partitionIds := make([]string, partitionCount)
	for i := range partitionIds {
		partitionIds[i] = fmt.Sprintf("%d", i)
	}

	key := hubKey(subID, rg, nsName, hubName)
	_, existedBefore, err := s.getHubRecord(subID, rg, nsName, hubName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	hub := EventHubResource{
		ID:   ns.ID + "/eventhubs/" + hubName,
		Name: hubName,
		Type: "Microsoft.EventHub/namespaces/eventhubs",
		Properties: EventHubProperties{
			ProvisioningState: "Succeeded",
			PartitionCount:    partitionCount,
			PartitionIds:      partitionIds,
		},
	}
	if err := s.db.Put(hubsBucket, key, hub); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneHub(hubEntityPath(nsName, hubName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, hub)
}

func (s *Service) getHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")

	hub, found, err := s.getHubRecord(subID, rg, nsName, hubName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el event hub '%s' no existe en el namespace '%s'", hubName, nsName))
		return
	}
	server.WriteJSON(w, http.StatusOK, hub)
}

func (s *Service) listHubs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")

	hubs := make([]EventHubResource, 0)
	err := s.db.List(hubsBucket, subID+"/"+rg+"/"+nsName+"/", func(_ string, raw []byte) error {
		var hub EventHubResource
		if err := json.Unmarshal(raw, &hub); err != nil {
			return err
		}
		hubs = append(hubs, hub)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": hubs})
}

func (s *Service) deleteHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")
	key := hubKey(subID, rg, nsName, hubName)

	found, err := s.db.Get(hubsBucket, key, &EventHubResource{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(hubsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.unmarkDataPlaneHub(hubEntityPath(nsName, hubName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getHubRecord(subID, rg, ns, name string) (EventHubResource, bool, error) {
	var hub EventHubResource
	found, err := s.db.Get(hubsBucket, hubKey(subID, rg, ns, name), &hub)
	return hub, found, err
}

func (s *Service) putConsumerGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")
	cgName := r.PathValue("consumerGroupName")

	hub, found, err := s.getHubRecord(subID, rg, nsName, hubName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el event hub '%s' no existe en el namespace '%s'", hubName, nsName))
		return
	}

	key := consumerGroupKey(subID, rg, nsName, hubName, cgName)
	_, existedBefore, err := s.getConsumerGroupRecord(subID, rg, nsName, hubName, cgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	cg := ConsumerGroup{
		ID:   hub.ID + "/consumergroups/" + cgName,
		Name: cgName,
		Type: "Microsoft.EventHub/namespaces/eventhubs/consumergroups",
		Properties: ConsumerGroupProperties{
			ProvisioningState: "Succeeded",
		},
	}
	if err := s.db.Put(consumerGroupsBucket, key, cg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, cg)
}

func (s *Service) getConsumerGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")
	cgName := r.PathValue("consumerGroupName")

	cg, found, err := s.getConsumerGroupRecord(subID, rg, nsName, hubName, cgName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el consumer group '%s' no existe en el event hub '%s'", cgName, hubName))
		return
	}
	server.WriteJSON(w, http.StatusOK, cg)
}

func (s *Service) listConsumerGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")

	cgs := make([]ConsumerGroup, 0)
	err := s.db.List(consumerGroupsBucket, subID+"/"+rg+"/"+nsName+"/"+hubName+"/", func(_ string, raw []byte) error {
		var cg ConsumerGroup
		if err := json.Unmarshal(raw, &cg); err != nil {
			return err
		}
		cgs = append(cgs, cg)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": cgs})
}

func (s *Service) deleteConsumerGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("eventHubName")
	cgName := r.PathValue("consumerGroupName")
	key := consumerGroupKey(subID, rg, nsName, hubName, cgName)

	found, err := s.db.Get(consumerGroupsBucket, key, &ConsumerGroup{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(consumerGroupsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getConsumerGroupRecord(subID, rg, ns, hub, name string) (ConsumerGroup, bool, error) {
	var cg ConsumerGroup
	found, err := s.db.Get(consumerGroupsBucket, consumerGroupKey(subID, rg, ns, hub, name), &cg)
	return cg, found, err
}
