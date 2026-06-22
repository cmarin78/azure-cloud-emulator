package apimanagement

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const servicesBucket = "apimanagement.services"

const apiManagementProvider = "Microsoft.ApiManagement"

// ApimService replica la forma estándar de ARM para
// Microsoft.ApiManagement/service.
type ApimService struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Location   string                `json:"location"`
	Tags       map[string]string     `json:"tags,omitempty"`
	SKU        ApimServiceSKU        `json:"sku"`
	Identity   *Identity             `json:"identity,omitempty"`
	Properties ApimServiceProperties `json:"properties"`
}

// ApimServiceSKU replica "sku" (azurerm_api_management's sku_name, p.ej.
// "Developer_1" o "Standard_2", se parsea como Name+"_"+Capacity en
// Terraform, pero ARM lo expone como objeto {name, capacity}).
type ApimServiceSKU struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

// Identity replica el bloque "identity" estándar de ARM, igual shape que
// usan AKS/App Service/Key Vault cuando se les asigna una managed identity.
type Identity struct {
	Type        string `json:"type"`
	PrincipalID string `json:"principalId,omitempty"`
	TenantID    string `json:"tenantId,omitempty"`
}

// ApimServiceProperties replica el subconjunto más usado de
// properties para una instancia de APIM.
type ApimServiceProperties struct {
	ProvisioningState       string   `json:"provisioningState"`
	PublisherEmail          string   `json:"publisherEmail"`
	PublisherName           string   `json:"publisherName"`
	NotificationSenderEmail string   `json:"notificationSenderEmail,omitempty"`
	GatewayURL              string   `json:"gatewayUrl"`
	GatewayRegionalURL      string   `json:"gatewayRegionalUrl,omitempty"`
	PortalURL               string   `json:"portalUrl"`
	DeveloperPortalURL      string   `json:"developerPortalUrl,omitempty"`
	ManagementAPIURL        string   `json:"managementApiUrl"`
	ScmURL                  string   `json:"scmUrl,omitempty"`
	PublicIPAddresses       []string `json:"publicIPAddresses,omitempty"`
	HostnameConfigurations  []any    `json:"hostnameConfigurations,omitempty"`
}

type apimServiceRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Identity   *Identity         `json:"identity,omitempty"`
	SKU        *ApimServiceSKU   `json:"sku,omitempty"`
	Properties struct {
		PublisherEmail          string `json:"publisherEmail"`
		PublisherName           string `json:"publisherName"`
		NotificationSenderEmail string `json:"notificationSenderEmail,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerServices(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service",
		s.listServices)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service/{serviceName}",
		s.putService)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service/{serviceName}",
		s.getServiceHandler)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service/{serviceName}",
		s.deleteService)
}

func serviceKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func serviceID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ApiManagement/service/%s", subID, rg, name)
}

// fakeHexSuffix deriva un sufijo hexadecimal determinista a partir de un
// seed, mismo espíritu que aks.fakeHexSuffix/network.fakePublicIP — cada
// paquete de este proyecto mantiene su propia copia de este helper en vez
// de compartir un paquete utilitario común.
func fakeHexSuffix(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

func fakeGUID(seed string) string {
	sum := fakeHexSuffix(seed)
	return fmt.Sprintf("%08x-0000-0000-0000-%012x", sum, uint64(sum)*2654435761)
}

// putService sigue el patrón "create-async" de aks/cluster.go: el recurso
// se construye completo con provisioningState "Succeeded" y se responde
// 202 con Azure-AsyncOperation/Location además del cuerpo — en Azure real
// aprovisionar una instancia de APIM tarda 30-45 minutos, así que aquí se
// modela como una LRO que siempre termina en éxito rápidamente.
func (s *Service) putService(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serviceName")

	var req apimServiceRequest
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
	if strings.TrimSpace(req.Properties.PublisherEmail) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.publisherEmail' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.PublisherName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.publisherName' es obligatorio")
		return
	}

	sku := ApimServiceSKU{Name: "Developer", Capacity: 1}
	if req.SKU != nil {
		sku = *req.SKU
		if sku.Capacity == 0 {
			sku.Capacity = 1
		}
	}

	id := serviceID(subID, rg, name)
	suffix := fakeHexSuffix(id)

	var identity *Identity
	if req.Identity != nil {
		identity = &Identity{Type: req.Identity.Type}
		if identity.Type != "" && identity.Type != "None" {
			identity.PrincipalID = fakeGUID(id + "-principal")
			identity.TenantID = fakeGUID(id + "-tenant")
		}
	}

	svc := ApimService{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.ApiManagement/service",
		Location: req.Location,
		Tags:     req.Tags,
		SKU:      sku,
		Identity: identity,
		Properties: ApimServiceProperties{
			ProvisioningState:       "Succeeded",
			PublisherEmail:          req.Properties.PublisherEmail,
			PublisherName:           req.Properties.PublisherName,
			NotificationSenderEmail: req.Properties.NotificationSenderEmail,
			GatewayURL:              fmt.Sprintf("https://%s-%08x.azure-api.net", name, suffix),
			PortalURL:               fmt.Sprintf("https://%s-%08x.portal.azure-api.net", name, suffix),
			DeveloperPortalURL:      fmt.Sprintf("https://%s-%08x.developer.azure-api.net", name, suffix),
			ManagementAPIURL:        fmt.Sprintf("https://%s-%08x.management.azure-api.net", name, suffix),
			ScmURL:                  fmt.Sprintf("https://%s-%08x.scm.azure-api.net", name, suffix),
			PublicIPAddresses:       []string{fmt.Sprintf("20.%d.%d.%d", suffix%256, (suffix>>8)%256, (suffix>>16)%256)},
		},
	}

	if err := s.db.Put(servicesBucket, serviceKey(subID, rg, name), svc); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	opID := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, apiManagementProvider, req.Location, opID, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, svc)
}

func (s *Service) getServiceHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serviceName")

	svc, found, err := s.getService(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de API Management '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, svc)
}

func (s *Service) listServices(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	services := make([]ApimService, 0)
	err := s.db.List(servicesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var svc ApimService
		if err := json.Unmarshal(raw, &svc); err != nil {
			return err
		}
		services = append(services, svc)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": services})
}

// deleteService sigue el patrón "delete-async" de aks/cluster.go: borra de
// forma síncrona (incluyendo todos sus apis/products/subscriptions) y
// responde con un 202 vacío.
func (s *Service) deleteService(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serviceName")

	svc, found, err := s.getService(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.deleteAllAPIsForService(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.deleteAllProductsForService(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.deleteAllSubscriptionsForService(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Delete(servicesBucket, serviceKey(subID, rg, name)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteAccepted(w, r, s.ops, subID, apiManagementProvider, svc.Location, apiVersion)
}

func (s *Service) getService(subID, rg, name string) (ApimService, bool, error) {
	var svc ApimService
	found, err := s.db.Get(servicesBucket, serviceKey(subID, rg, name), &svc)
	return svc, found, err
}

// serviceExists es un helper liviano para que apis.go/products.go validen
// que el servicio padre existe antes de crear un sub-recurso, igual que
// el resto del proyecto no valida referencias cruzadas entre paquetes
// distintos (p.ej. App Service no valida que serverFarmId exista) pero sí
// valida jerarquías dentro del mismo paquete (AKS valida que el cluster
// exista antes de aceptar un agentPool).
func (s *Service) serviceExists(subID, rg, name string) (bool, error) {
	_, found, err := s.getService(subID, rg, name)
	return found, err
}
