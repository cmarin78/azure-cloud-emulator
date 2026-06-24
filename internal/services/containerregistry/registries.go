package containerregistry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const registriesBucket = "containerregistry.registries"

// Registry replica el subconjunto relevante de
// Microsoft.ContainerRegistry/registries que az/Terraform
// (azurerm_container_registry) leen.
type Registry struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Location   string             `json:"location"`
	Tags       map[string]string  `json:"tags,omitempty"`
	Sku        RegistrySku        `json:"sku"`
	Properties RegistryProperties `json:"properties"`
}

type RegistrySku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// RegistryProperties incluye, además de los campos básicos que el emulador
// realmente modela (loginServer, adminUserEnabled), un conjunto de
// sub-objetos que Azure real siempre devuelve poblados (policies,
// encryption, networkRuleSet) aunque el SKU no los soporte de verdad
// (p. ej. Basic no admite reglas de red). Se incluyen aquí -- con valores
// "apagados" pero presentes -- porque azurerm_container_registry los lee
// sin chequear nil (ver containers.resourceContainerRegistryRead en el
// provider real); omitirlos hace que el provider entre en panic con un
// nil pointer dereference en vez de fallar limpiamente.
type RegistryProperties struct {
	LoginServer              string                 `json:"loginServer"`
	ProvisioningState        string                 `json:"provisioningState"`
	AdminUserEnabled         bool                   `json:"adminUserEnabled"`
	CreationDate             string                 `json:"creationDate,omitempty"`
	PublicNetworkAccess      string                 `json:"publicNetworkAccess"`
	NetworkRuleBypassOptions string                 `json:"networkRuleBypassOptions"`
	ZoneRedundancy           string                 `json:"zoneRedundancy"`
	AnonymousPullEnabled     bool                   `json:"anonymousPullEnabled"`
	DataEndpointEnabled      bool                   `json:"dataEndpointEnabled"`
	NetworkRuleSet           RegistryNetworkRuleSet `json:"networkRuleSet"`
	Policies                 RegistryPolicies       `json:"policies"`
	Encryption               RegistryEncryption     `json:"encryption"`
}

// RegistryNetworkRuleSet incluye ipRules/virtualNetworkRules como arrays
// vacíos (no omitidos) a propósito: flattenNetworkRuleSet en el provider
// real (container_registry_resource.go) hace
// `for _, ipRule := range *networkRuleSet.IPRules` sin chequear nil -- si
// el campo "ipRules" falta en el JSON, el puntero *[]IPRule del SDK queda
// nil y esa dereferencia hace panic al provider entero.
type RegistryNetworkRuleSet struct {
	DefaultAction       string                       `json:"defaultAction"`
	IPRules             []RegistryIPRule             `json:"ipRules"`
	VirtualNetworkRules []RegistryVirtualNetworkRule `json:"virtualNetworkRules"`
}

type RegistryIPRule struct {
	Action string `json:"action"`
	Value  string `json:"value"`
}

type RegistryVirtualNetworkRule struct {
	Action string `json:"action"`
	ID     string `json:"id"`
}

type RegistryPolicies struct {
	QuarantinePolicy RegistryPolicyStatus    `json:"quarantinePolicy"`
	TrustPolicy      RegistryTrustPolicy     `json:"trustPolicy"`
	RetentionPolicy  RegistryRetentionPolicy `json:"retentionPolicy"`
	ExportPolicy     RegistryPolicyStatus    `json:"exportPolicy"`
}

type RegistryPolicyStatus struct {
	Status string `json:"status"`
}

type RegistryTrustPolicy struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type RegistryRetentionPolicy struct {
	Days   int    `json:"days"`
	Status string `json:"status"`
}

type RegistryEncryption struct {
	Status string `json:"status"`
}

type registryRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        RegistrySku       `json:"sku"`
	Properties struct {
		AdminUserEnabled     bool   `json:"adminUserEnabled"`
		AnonymousPullEnabled bool   `json:"anonymousPullEnabled"`
		DataEndpointEnabled  bool   `json:"dataEndpointEnabled"`
		PublicNetworkAccess  string `json:"publicNetworkAccess"`
	} `json:"properties"`
}

// defaultRegistryProperties devuelve los sub-objetos "apagados pero
// presentes" descritos en el comentario de RegistryProperties.
func defaultRegistryProperties() (RegistryNetworkRuleSet, RegistryPolicies, RegistryEncryption) {
	return RegistryNetworkRuleSet{
			DefaultAction:       "Allow",
			IPRules:             []RegistryIPRule{},
			VirtualNetworkRules: []RegistryVirtualNetworkRule{},
		},
		RegistryPolicies{
			QuarantinePolicy: RegistryPolicyStatus{Status: "disabled"},
			TrustPolicy:      RegistryTrustPolicy{Type: "Notary", Status: "disabled"},
			RetentionPolicy:  RegistryRetentionPolicy{Days: 7, Status: "disabled"},
			ExportPolicy:     RegistryPolicyStatus{Status: "enabled"},
		},
		RegistryEncryption{Status: "disabled"}
}

func registryKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func registryID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerRegistry/registries/%s", subID, rg, name)
}

// fakeLoginServer deriva el hostname del registry del mismo modo que Azure
// real lo hace: el nombre del registry es único dentro del namespace
// global de ACR, así que loginServer es simplemente "{name}.azurecr.io" en
// minúsculas -- sin necesidad de un sufijo aleatorio como en storage
// accounts.
func fakeLoginServer(name string) string {
	return strings.ToLower(name) + ".azurecr.io"
}

func (s *Service) registerRegistries(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerRegistry/registries"
	mux.HandleFunc("GET "+base, s.listRegistries)
	mux.HandleFunc("PUT "+base+"/{registryName}", s.putRegistry)
	mux.HandleFunc("GET "+base+"/{registryName}", s.getRegistryHandler)
	mux.HandleFunc("DELETE "+base+"/{registryName}", s.deleteRegistry)

	// listCredentials: azurerm_container_registry lo llama en su Read
	// siempre que admin_enabled=true, para poblar admin_username/
	// admin_password. No hay credenciales reales detrás de este emulador
	// -- se generan valores fake estables (mismo patrón "shape-compatible,
	// no behavior-complete" que el resto del servicio).
	mux.HandleFunc("POST "+base+"/{registryName}/listCredentials", s.listCredentials)

	// replications: azurerm_container_registry siempre consulta esta
	// colección en su Read (para reconciliar el atributo "georeplications"),
	// incluso si el registry nunca configuró geo-replicación. El emulador no
	// soporta replicas reales -- siempre responde una lista vacía.
	mux.HandleFunc("GET "+base+"/{registryName}/replications", s.listReplications)

	// checkNameAvailability vive a nivel de subscription (no de resource
	// group), igual que en Azure real: ACR tiene un namespace global de
	// nombres (loginServer = {name}.azurecr.io), así que la disponibilidad
	// no depende del resource group destino. azurerm_container_registry
	// llama a este endpoint antes de cada PUT.
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/providers/Microsoft.ContainerRegistry/checkNameAvailability", s.checkNameAvailability)
}

type checkNameAvailabilityRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type checkNameAvailabilityResponse struct {
	NameAvailable bool   `json:"nameAvailable"`
	Reason        string `json:"reason,omitempty"`
	Message       string `json:"message,omitempty"`
}

// checkNameAvailability no simula colisiones de nombre reales (no hay un
// namespace global de verdad detrás del emulador): siempre responde
// nameAvailable=true salvo que el nombre ya esté en uso por un registry
// existente en cualquier resource group de la misma subscription --
// "shape-compatible, no behavior-complete", igual que el resto del
// servicio.
func (s *Service) checkNameAvailability(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")

	var req checkNameAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	taken := false
	err := s.db.List(registriesBucket, subID+"/", func(_ string, raw []byte) error {
		var reg Registry
		if err := json.Unmarshal(raw, &reg); err != nil {
			return err
		}
		if strings.EqualFold(reg.Name, req.Name) {
			taken = true
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	resp := checkNameAvailabilityResponse{NameAvailable: !taken}
	if taken {
		resp.Reason = "AlreadyExists"
		resp.Message = fmt.Sprintf("el nombre '%s' ya está en uso por otro container registry", req.Name)
	}
	server.WriteJSON(w, http.StatusOK, resp)
}

// putRegistry es síncrono (Effort "S", igual que keyvault.Vault/
// managedidentity.UserAssignedIdentity): crear un registry en Azure real no
// requiere polling en los flujos comunes de az CLI/Terraform.
type listCredentialsResponse struct {
	Username  string                  `json:"username"`
	Passwords []registryPasswordEntry `json:"passwords"`
}

type registryPasswordEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// listCredentials genera credenciales fake estables (derivadas del nombre
// del registry) si éste existe y tiene adminUserEnabled=true; 404 si el
// registry no existe.
func (s *Service) listCredentials(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("registryName")

	reg, found, err := s.getRegistry(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el container registry '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	resp := listCredentialsResponse{
		Username: strings.ToLower(reg.Name),
		Passwords: []registryPasswordEntry{
			{Name: "password", Value: fmt.Sprintf("%s-fake-password-1", strings.ToLower(reg.Name))},
			{Name: "password2", Value: fmt.Sprintf("%s-fake-password-2", strings.ToLower(reg.Name))},
		},
	}
	server.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) listReplications(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("registryName")

	if _, found, err := s.getRegistry(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el container registry '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []any{}})
}

func (s *Service) putRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("registryName")

	var req registryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Sku.Name) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'sku.name' es obligatorio")
		return
	}

	key := registryKey(subID, rg, name)
	existing, found, err := s.getRegistry(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	creationDate := time.Now().UTC().Format(time.RFC3339)
	if found {
		creationDate = existing.Properties.CreationDate
	}

	sku := req.Sku
	if sku.Tier == "" {
		sku.Tier = sku.Name
	}

	publicAccess := req.Properties.PublicNetworkAccess
	if publicAccess == "" {
		publicAccess = "Enabled"
	}
	networkRuleSet, policies, encryption := defaultRegistryProperties()

	reg := Registry{
		ID:       registryID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.ContainerRegistry/registries",
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: RegistryProperties{
			LoginServer:              fakeLoginServer(name),
			ProvisioningState:        "Succeeded",
			AdminUserEnabled:         req.Properties.AdminUserEnabled,
			CreationDate:             creationDate,
			PublicNetworkAccess:      publicAccess,
			NetworkRuleBypassOptions: "AzureServices",
			ZoneRedundancy:           "Disabled",
			AnonymousPullEnabled:     req.Properties.AnonymousPullEnabled,
			DataEndpointEnabled:      req.Properties.DataEndpointEnabled,
			NetworkRuleSet:           networkRuleSet,
			Policies:                 policies,
			Encryption:               encryption,
		},
	}

	if err := s.db.Put(registriesBucket, key, reg); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, reg)
}

func (s *Service) getRegistry(subID, rg, name string) (Registry, bool, error) {
	var reg Registry
	found, err := s.db.Get(registriesBucket, registryKey(subID, rg, name), &reg)
	return reg, found, err
}

func (s *Service) getRegistryHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("registryName")

	reg, found, err := s.getRegistry(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el container registry '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, reg)
}

func (s *Service) listRegistries(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	regs := make([]Registry, 0)
	err := s.db.List(registriesBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
		var reg Registry
		if err := json.Unmarshal(raw, &reg); err != nil {
			return err
		}
		regs = append(regs, reg)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": regs})
}

// deleteRegistry es idempotente (204 si no existe) y síncrono.
func (s *Service) deleteRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("registryName")
	key := registryKey(subID, rg, name)

	found, err := s.db.Get(registriesBucket, key, &Registry{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(registriesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
