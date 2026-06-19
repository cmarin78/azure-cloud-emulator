package servicebus

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const topicsBucket = "servicebus.topics"
const subscriptionsBucket = "servicebus.subscriptions"

// Topic replica la forma estándar de ARM para
// Microsoft.ServiceBus/namespaces/topics.
type Topic struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Properties TopicProperties `json:"properties"`
}

type TopicProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

// Subscription replica la forma estándar de ARM para
// Microsoft.ServiceBus/namespaces/topics/subscriptions.
type Subscription struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties SubscriptionProperties `json:"properties"`
}

type SubscriptionProperties struct {
	ProvisioningState string `json:"provisioningState"`
}

func (s *Service) registerTopics(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics",
		s.listTopics)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}",
		s.putTopic)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}",
		s.getTopic)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}",
		s.deleteTopic)

	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions",
		s.listSubscriptions)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}",
		s.putSubscription)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}",
		s.getSubscription)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ServiceBus/namespaces/{namespaceName}/topics/{topicName}/subscriptions/{subscriptionName}",
		s.deleteSubscription)
}

func topicKey(subID, rg, ns, topic string) string {
	return subID + "/" + rg + "/" + ns + "/" + topic
}

func subscriptionKey(subID, rg, ns, topic, sub string) string {
	return subID + "/" + rg + "/" + ns + "/" + topic + "/" + sub
}

func (s *Service) putTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")

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

	key := topicKey(subID, rg, nsName, topicName)
	_, existedBefore, err := s.getTopicRecord(subID, rg, nsName, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	t := Topic{
		ID:   ns.ID + "/topics/" + topicName,
		Name: topicName,
		Type: "Microsoft.ServiceBus/namespaces/topics",
		Properties: TopicProperties{
			ProvisioningState: "Succeeded",
		},
	}
	if err := s.db.Put(topicsBucket, key, t); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneEntity(topicEntityPath(nsName, topicName)); err != nil {
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
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")

	t, found, err := s.getTopicRecord(subID, rg, nsName, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el topic '%s' no existe en el namespace '%s'", topicName, nsName))
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
	nsName := r.PathValue("namespaceName")

	topics := make([]Topic, 0)
	err := s.db.List(topicsBucket, subID+"/"+rg+"/"+nsName+"/", func(key string, raw []byte) error {
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

func (s *Service) deleteTopic(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")
	key := topicKey(subID, rg, nsName, topicName)

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
	if err := s.unmarkDataPlaneEntity(topicEntityPath(nsName, topicName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getTopicRecord(subID, rg, ns, name string) (Topic, bool, error) {
	var t Topic
	found, err := s.db.Get(topicsBucket, topicKey(subID, rg, ns, name), &t)
	return t, found, err
}

func (s *Service) putSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")
	subName := r.PathValue("subscriptionName")

	topic, found, err := s.getTopicRecord(subID, rg, nsName, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el topic '%s' no existe en el namespace '%s'", topicName, nsName))
		return
	}

	key := subscriptionKey(subID, rg, nsName, topicName, subName)
	_, existedBefore, err := s.getSubscriptionRecord(subID, rg, nsName, topicName, subName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	sub := Subscription{
		ID:   topic.ID + "/subscriptions/" + subName,
		Name: subName,
		Type: "Microsoft.ServiceBus/namespaces/topics/subscriptions",
		Properties: SubscriptionProperties{
			ProvisioningState: "Succeeded",
		},
	}
	if err := s.db.Put(subscriptionsBucket, key, sub); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.markDataPlaneEntity(subscriptionEntityPath(nsName, topicName, subName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, sub)
}

func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")
	subName := r.PathValue("subscriptionName")

	sub, found, err := s.getSubscriptionRecord(subID, rg, nsName, topicName, subName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la subscription '%s' no existe en el topic '%s'", subName, topicName))
		return
	}
	server.WriteJSON(w, http.StatusOK, sub)
}

func (s *Service) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")

	subs := make([]Subscription, 0)
	err := s.db.List(subscriptionsBucket, subID+"/"+rg+"/"+nsName+"/"+topicName+"/", func(key string, raw []byte) error {
		var sub Subscription
		if err := json.Unmarshal(raw, &sub); err != nil {
			return err
		}
		subs = append(subs, sub)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": subs})
}

func (s *Service) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	topicName := r.PathValue("topicName")
	subName := r.PathValue("subscriptionName")
	key := subscriptionKey(subID, rg, nsName, topicName, subName)

	found, err := s.db.Get(subscriptionsBucket, key, &Subscription{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(subscriptionsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.unmarkDataPlaneEntity(subscriptionEntityPath(nsName, topicName, subName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getSubscriptionRecord(subID, rg, ns, topic, name string) (Subscription, bool, error) {
	var sub Subscription
	found, err := s.db.Get(subscriptionsBucket, subscriptionKey(subID, rg, ns, topic, name), &sub)
	return sub, found, err
}
