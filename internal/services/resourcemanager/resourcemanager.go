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
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/resources", s.listResourcesInGroup)

	mux.HandleFunc("GET /subscriptions/{subscriptionId}/providers", s.listProviders)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/providers/{providerNamespace}", s.getProvider)
}

// Provider replica la forma mínima de un resource provider de ARM (lo que
// devuelve GET /subscriptions/{id}/providers[/{namespace}]). El provider
// `azurerm` de Terraform consulta este endpoint al iniciar para construir
// su caché de registro: si el namespace que va a usar no aparece como
// "Registered", falla el plan/apply antes de llegar a crear ningún recurso.
// registeredNamespaces cubre exactamente los providers que el emulador
// implementa hoy; cualquier otro namespace responde "NotRegistered".
type Provider struct {
	ID                string                 `json:"id"`
	Namespace         string                 `json:"namespace"`
	RegistrationState string                 `json:"registrationState"`
	ResourceTypes     []ProviderResourceType `json:"resourceTypes"`
}

// ProviderResourceType replica el shape mínimo de cada tipo de recurso
// dentro de un provider. Azure real devuelve mucho más detalle (locations,
// apiVersions, capabilities...); aquí basta con lo que algunas
// herramientas leen para decidir si el tipo existe en este provider.
type ProviderResourceType struct {
	ResourceType string   `json:"resourceType"`
	Locations    []string `json:"locations"`
}

var registeredNamespaces = []Provider{
	{Namespace: "Microsoft.Resources", ResourceTypes: []ProviderResourceType{
		{ResourceType: "resourceGroups", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "deployments", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.Storage", ResourceTypes: []ProviderResourceType{
		{ResourceType: "storageAccounts", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.Network", ResourceTypes: []ProviderResourceType{
		{ResourceType: "virtualNetworks", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "networkInterfaces", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "networkSecurityGroups", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "publicIPAddresses", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "loadBalancers", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "routeTables", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "privateDnsZones", Locations: []string{"global"}},
	}},
	{Namespace: "Microsoft.Compute", ResourceTypes: []ProviderResourceType{
		{ResourceType: "disks", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "virtualMachines", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.KeyVault", ResourceTypes: []ProviderResourceType{
		{ResourceType: "vaults", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.ServiceBus", ResourceTypes: []ProviderResourceType{
		{ResourceType: "namespaces", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.DocumentDB", ResourceTypes: []ProviderResourceType{
		{ResourceType: "databaseAccounts", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.OperationalInsights", ResourceTypes: []ProviderResourceType{
		{ResourceType: "workspaces", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.Insights", ResourceTypes: []ProviderResourceType{
		{ResourceType: "actionGroups", Locations: []string{"global"}},
		{ResourceType: "metricAlerts", Locations: []string{"global"}},
	}},
	{Namespace: "Microsoft.Web", ResourceTypes: []ProviderResourceType{
		{ResourceType: "serverfarms", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "sites", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.ContainerService", ResourceTypes: []ProviderResourceType{
		{ResourceType: "managedClusters", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.Authorization", ResourceTypes: []ProviderResourceType{
		{ResourceType: "roleDefinitions", Locations: []string{"global"}},
		{ResourceType: "roleAssignments", Locations: []string{"global"}},
	}},
	{Namespace: "Microsoft.ManagedIdentity", ResourceTypes: []ProviderResourceType{
		{ResourceType: "userAssignedIdentities", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.EventGrid", ResourceTypes: []ProviderResourceType{
		{ResourceType: "topics", Locations: []string{"eastus", "westus2"}},
		{ResourceType: "eventSubscriptions", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.EventHub", ResourceTypes: []ProviderResourceType{
		{ResourceType: "namespaces", Locations: []string{"eastus", "westus2"}},
	}},
	{Namespace: "Microsoft.ApiManagement", ResourceTypes: []ProviderResourceType{
		{ResourceType: "service", Locations: []string{"eastus", "westus2"}},
	}},
}

func (s *Service) listProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")

	providers := make([]Provider, 0, len(registeredNamespaces))
	for _, p := range registeredNamespaces {
		p.ID = fmt.Sprintf("/subscriptions/%s/providers/%s", subID, p.Namespace)
		p.RegistrationState = "Registered"
		providers = append(providers, p)
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": providers})
}

func (s *Service) getProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	namespace := r.PathValue("providerNamespace")

	for _, p := range registeredNamespaces {
		if strings.EqualFold(p.Namespace, namespace) {
			p.ID = fmt.Sprintf("/subscriptions/%s/providers/%s", subID, p.Namespace)
			p.RegistrationState = "Registered"
			server.WriteJSON(w, http.StatusOK, p)
			return
		}
	}

	// Namespace desconocido: se modela como "NotRegistered" en vez de 404,
	// igual que Azure real (el namespace "existe" conceptualmente, solo que
	// no está registrado en la suscripción).
	server.WriteJSON(w, http.StatusOK, Provider{
		ID:                fmt.Sprintf("/subscriptions/%s/providers/%s", subID, namespace),
		Namespace:         namespace,
		RegistrationState: "NotRegistered",
		ResourceTypes:     []ProviderResourceType{},
	})
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

// listResourcesInGroup emula GET .../resourceGroups/{rg}/resources
// (Microsoft.Resources/resources, listado genérico cross-provider). El
// provider de Terraform `azurerm` lo consulta antes de borrar un resource
// group, para decidir el orden de borrado de los recursos que contiene.
// Este emulador no mantiene un índice genérico de todos los recursos de
// cada servicio (cada paquete tiene su propio bucket de BoltDB), así que
// simplemente respondemos una lista vacía: es exacto para el caso de uso
// real de las pruebas de este proyecto (resource groups vacíos o cuyos
// recursos se borran explícitamente antes del resource group) y evita el
// 404 que rompía `terraform destroy`.
func (s *Service) listResourcesInGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
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
