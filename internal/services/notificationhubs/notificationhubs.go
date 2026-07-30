// Package notificationhubs implementa Microsoft.NotificationHubs/namespaces
// y los hubs anidados (ARM CRUD sync). Los valores de metadata
// (connectionString, pnsCredentials) son deterministas para dar cero drift
// en terraform plan con azurerm_notification_hub_namespace /
// azurerm_notification_hub.
package notificationhubs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	nsBucket  = "notificationhubs.namespaces"
	hubBucket = "notificationhubs.hubs"
	nsBase    = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.NotificationHubs/namespaces"
	nsType    = "Microsoft.NotificationHubs/namespaces"
	hubType   = "Microsoft.NotificationHubs/namespaces/notificationHubs"
)

// Service expone las rutas ARM para Notification Hubs.
type Service struct{ db *storage.DB }

// New crea una instancia del servicio.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register registra todas las rutas ARM.
func (s *Service) Register(mux *http.ServeMux) {
	// Namespace CRUD
	mux.HandleFunc("GET "+nsBase, s.listNamespaces)
	mux.HandleFunc("PUT "+nsBase+"/{namespaceName}", s.putNamespace)
	mux.HandleFunc("GET "+nsBase+"/{namespaceName}", s.getNamespace)
	mux.HandleFunc("DELETE "+nsBase+"/{namespaceName}", s.deleteNamespace)
	// Hub CRUD dentro del namespace
	hubBase := nsBase + "/{namespaceName}/notificationHubs"
	mux.HandleFunc("GET "+hubBase, s.listHubs)
	mux.HandleFunc("PUT "+hubBase+"/{hubName}", s.putHub)
	mux.HandleFunc("GET "+hubBase+"/{hubName}", s.getHub)
	mux.HandleFunc("DELETE "+hubBase+"/{hubName}", s.deleteHub)
	// listKeys: azurerm_notification_hub_authorization_rule y otros lo llaman
	// para obtener connection strings; devolvemos valores deterministas.
	mux.HandleFunc("POST "+hubBase+"/{hubName}/authorizationRules/{ruleName}/listKeys", s.listHubKeys)
	// AuthorizationRules en el hub
	mux.HandleFunc("GET "+hubBase+"/{hubName}/authorizationRules", s.listHubAuthRules)
	mux.HandleFunc("PUT "+hubBase+"/{hubName}/authorizationRules/{ruleName}", s.putHubAuthRule)
	mux.HandleFunc("GET "+hubBase+"/{hubName}/authorizationRules/{ruleName}", s.getHubAuthRule)
	mux.HandleFunc("DELETE "+hubBase+"/{hubName}/authorizationRules/{ruleName}", s.deleteHubAuthRule)
}

// ── Namespace ──────────────────────────────────────────────────────────────

// Namespace replica el shape que azurerm_notification_hub_namespace lee.
type Namespace struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        NamespaceSku      `json:"sku"`
	Properties NamespaceProps    `json:"properties"`
}

type NamespaceSku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

type NamespaceProps struct {
	NamespaceType    string `json:"namespaceType"`
	ProvisioningState string `json:"provisioningState"`
	Status           string `json:"status"`
	ServiceBusEndpoint string `json:"serviceBusEndpoint"`
}

type nsRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *NamespaceSku     `json:"sku,omitempty"`
	Properties struct {
		NamespaceType string `json:"namespaceType"`
	} `json:"properties"`
}

func nsKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func nsID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.NotificationHubs/namespaces/%s", sub, rg, name)
}
func fakeServiceBusEndpoint(name string) string {
	return "sb://" + strings.ToLower(name) + ".servicebus.windows.net/"
}

func (s *Service) putNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")

	var req nsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := nsKey(sub, rg, name)
	var existing Namespace
	found, _ := s.db.Get(nsBucket, key, &existing)

	sku := NamespaceSku{Name: "Free", Tier: "Free"}
	if req.Sku != nil && req.Sku.Name != "" {
		sku = *req.Sku
		if sku.Tier == "" {
			sku.Tier = sku.Name
		}
	}
	nsType := req.Properties.NamespaceType
	if nsType == "" {
		nsType = "NotificationHub"
	}

	ns := Namespace{
		ID:       nsID(sub, rg, name),
		Name:     name,
		Type:     nsType,
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: NamespaceProps{
			NamespaceType:      nsType,
			ProvisioningState:  "Succeeded",
			Status:             "Active",
			ServiceBusEndpoint: fakeServiceBusEndpoint(name),
		},
	}
	if err := s.db.Put(nsBucket, key, ns); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, ns)
}

func (s *Service) loadNamespace(key string) (Namespace, bool, error) {
	var ns Namespace
	found, err := s.db.Get(nsBucket, key, &ns)
	return ns, found, err
}

func (s *Service) getNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")

	ns, found, err := s.loadNamespace(nsKey(sub, rg, name))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("namespace '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, ns)
}

func (s *Service) listNamespaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	items := make([]Namespace, 0)
	_ = s.db.List(nsBucket, sub+"/"+rg+"/", func(_ string, raw []byte) error {
		var ns Namespace
		if err := json.Unmarshal(raw, &ns); err == nil {
			items = append(items, ns)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("namespaceName")
	_ = s.db.Delete(nsBucket, nsKey(sub, rg, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Notification Hub ───────────────────────────────────────────────────────

// Hub replica el shape que azurerm_notification_hub lee.
type Hub struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties HubProperties     `json:"properties"`
}

type HubProperties struct {
	RegistrationTTL string `json:"registrationTtl,omitempty"`
}

func hubKey(sub, rg, nsName, name string) string { return sub + "/" + rg + "/" + nsName + "/" + name }
func hubID(sub, rg, nsName, name string) string {
	return nsID(sub, rg, nsName) + "/notificationHubs/" + name
}

type hubRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties *HubProperties    `json:"properties,omitempty"`
}

func (s *Service) putHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	name := r.PathValue("hubName")

	var req hubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := hubKey(sub, rg, nsName, name)
	var existing Hub
	found, _ := s.db.Get(hubBucket, key, &existing)

	props := HubProperties{RegistrationTTL: "10675199.02:48:05.4775807"}
	if req.Properties != nil && req.Properties.RegistrationTTL != "" {
		props.RegistrationTTL = req.Properties.RegistrationTTL
	}

	hub := Hub{
		ID:         hubID(sub, rg, nsName, name),
		Name:       name,
		Type:       hubType,
		Location:   req.Location,
		Tags:       req.Tags,
		Properties: props,
	}
	if err := s.db.Put(hubBucket, key, hub); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, hub)
}

func (s *Service) getHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	name := r.PathValue("hubName")

	var hub Hub
	found, err := s.db.Get(hubBucket, hubKey(sub, rg, nsName, name), &hub)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("notification hub '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, hub)
}

func (s *Service) listHubs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	items := make([]Hub, 0)
	_ = s.db.List(hubBucket, sub+"/"+rg+"/"+nsName+"/", func(_ string, raw []byte) error {
		var hub Hub
		if err := json.Unmarshal(raw, &hub); err == nil {
			items = append(items, hub)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteHub(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	name := r.PathValue("hubName")
	_ = s.db.Delete(hubBucket, hubKey(sub, rg, nsName, name))
	w.WriteHeader(http.StatusNoContent)
}

// ── Authorization Rules + listKeys ─────────────────────────────────────────

// authRuleBucket usa la misma BoltDB pero con prefijo diferente.
const authRuleBucket = "notificationhubs.authrules"

// AuthRule representa una regla de autorización en un hub.
type AuthRule struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Properties AuthRuleProperties `json:"properties"`
}

type AuthRuleProperties struct {
	Rights []string `json:"rights"`
}

func authRuleKey(sub, rg, ns, hub, rule string) string {
	return sub + "/" + rg + "/" + ns + "/" + hub + "/" + rule
}
func authRuleID(sub, rg, ns, hub, rule string) string {
	return hubID(sub, rg, ns, hub) + "/authorizationRules/" + rule
}

func (s *Service) putHubAuthRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("hubName")
	ruleName := r.PathValue("ruleName")

	var req struct {
		Properties AuthRuleProperties `json:"properties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	key := authRuleKey(sub, rg, nsName, hubName, ruleName)
	var existing AuthRule
	found, _ := s.db.Get(authRuleBucket, key, &existing)

	rule := AuthRule{
		ID:         authRuleID(sub, rg, nsName, hubName, ruleName),
		Name:       ruleName,
		Type:       "Microsoft.NotificationHubs/namespaces/notificationHubs/authorizationRules",
		Properties: req.Properties,
	}
	if err := s.db.Put(authRuleBucket, key, rule); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rule)
}

func (s *Service) getHubAuthRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("hubName")
	ruleName := r.PathValue("ruleName")

	var rule AuthRule
	found, err := s.db.Get(authRuleBucket, authRuleKey(sub, rg, nsName, hubName, ruleName), &rule)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("authorization rule '%s' no encontrada", ruleName))
		return
	}
	server.WriteJSON(w, http.StatusOK, rule)
}

func (s *Service) listHubAuthRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("hubName")
	items := make([]AuthRule, 0)
	_ = s.db.List(authRuleBucket, sub+"/"+rg+"/"+nsName+"/"+hubName+"/", func(_ string, raw []byte) error {
		var rule AuthRule
		if err := json.Unmarshal(raw, &rule); err == nil {
			items = append(items, rule)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteHubAuthRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("hubName")
	ruleName := r.PathValue("ruleName")
	_ = s.db.Delete(authRuleBucket, authRuleKey(sub, rg, nsName, hubName, ruleName))
	w.WriteHeader(http.StatusNoContent)
}

// listHubKeys devuelve connection strings deterministas para el hub.
// azurerm_notification_hub_authorization_rule llama a este endpoint para leer
// primaryConnectionString / secondaryConnectionString.
func (s *Service) listHubKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	nsName := r.PathValue("namespaceName")
	hubName := r.PathValue("hubName")
	ruleName := r.PathValue("ruleName")

	// Connection string determinista: {endpoint};SharedAccessKeyName={rule};SharedAccessKey={fakehex}
	endpoint := "sb://" + strings.ToLower(nsName) + ".servicebus.windows.net/"
	fakeKey := fmt.Sprintf("fakekey%s%s%s00000000000000000000000000000000", nsName, hubName, ruleName)
	if len(fakeKey) > 44 {
		fakeKey = fakeKey[:44]
	}
	primary := fmt.Sprintf("Endpoint=%s;SharedAccessKeyName=%s;SharedAccessKey=%s=", endpoint, ruleName, fakeKey)
	secondary := fmt.Sprintf("Endpoint=%s;SharedAccessKeyName=%s;SharedAccessKey=%s=", endpoint, ruleName, fakeKey+"sec")

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"primaryConnectionString":            primary,
		"secondaryConnectionString":          secondary,
		"primaryKey":                         fakeKey + "=",
		"secondaryKey":                       fakeKey + "sec=",
		"keyName":                            ruleName,
		"claimType":                          "SharedAccessKey",
		"claimValue":                         "None",
		"rights":                             []string{"Listen", "Manage", "Send"},
		"createdTime":                        "2024-01-01T00:00:00.000Z",
		"modifiedTime":                       "2024-01-01T00:00:00.000Z",
		"revision":                           1,
	})
}
