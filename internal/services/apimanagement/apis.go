package apimanagement

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const apisBucket = "apimanagement.apis"
const operationsBucket = "apimanagement.operations"

// Api replica la forma estándar de ARM para
// Microsoft.ApiManagement/service/apis. Sub-recurso síncrono: a diferencia
// de la instancia de servicio, crear/borrar una API no tiene ningún costo
// de aprovisionamiento real detrás (no hay gateway real al que publicar),
// así que se modela igual que las reglas de seguridad de un NSG o los
// topics de Event Grid.
type Api struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Properties ApiProperties `json:"properties"`
}

// ApiProperties replica el subconjunto más usado de properties para una
// API publicada en APIM.
type ApiProperties struct {
	DisplayName          string   `json:"displayName"`
	APIRevision          string   `json:"apiRevision,omitempty"`
	Description          string   `json:"description,omitempty"`
	Path                 string   `json:"path"`
	Protocols            []string `json:"protocols,omitempty"`
	ServiceURL           string   `json:"serviceUrl,omitempty"`
	APIType              string   `json:"apiType,omitempty"`
	SubscriptionRequired *bool    `json:"subscriptionRequired,omitempty"`
	IsCurrent            bool     `json:"isCurrent"`
}

type apiRequest struct {
	Properties struct {
		DisplayName          string   `json:"displayName"`
		Description          string   `json:"description,omitempty"`
		Path                 string   `json:"path"`
		Protocols            []string `json:"protocols,omitempty"`
		ServiceURL           string   `json:"serviceUrl,omitempty"`
		APIType              string   `json:"apiType,omitempty"`
		SubscriptionRequired *bool    `json:"subscriptionRequired,omitempty"`
	} `json:"properties"`
}

// ApiOperation replica la forma estándar de ARM para
// Microsoft.ApiManagement/service/apis/operations.
type ApiOperation struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties ApiOperationProperties `json:"properties"`
}

// ApiOperationProperties replica el subconjunto más usado de properties
// para una operación dentro de una API.
type ApiOperationProperties struct {
	DisplayName string `json:"displayName"`
	Method      string `json:"method"`
	URLTemplate string `json:"urlTemplate"`
	Description string `json:"description,omitempty"`
}

type apiOperationRequest struct {
	Properties struct {
		DisplayName string `json:"displayName"`
		Method      string `json:"method"`
		URLTemplate string `json:"urlTemplate"`
		Description string `json:"description,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerAPIs(mux *http.ServeMux) {
	const base = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service/{serviceName}/apis"

	mux.HandleFunc("GET "+base, s.listAPIs)
	mux.HandleFunc("PUT "+base+"/{apiId}", s.putAPI)
	mux.HandleFunc("GET "+base+"/{apiId}", s.getAPIHandler)
	mux.HandleFunc("DELETE "+base+"/{apiId}", s.deleteAPI)

	mux.HandleFunc("GET "+base+"/{apiId}/operations", s.listOperations)
	mux.HandleFunc("PUT "+base+"/{apiId}/operations/{operationId}", s.putOperation)
	mux.HandleFunc("GET "+base+"/{apiId}/operations/{operationId}", s.getOperationHandler)
	mux.HandleFunc("DELETE "+base+"/{apiId}/operations/{operationId}", s.deleteOperation)
}

func apiKey(subID, rg, svc, apiID string) string {
	return subID + "/" + rg + "/" + svc + "/" + apiID
}

func apiResourceID(subID, rg, svc, apiID string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ApiManagement/service/%s/apis/%s", subID, rg, svc, apiID)
}

func operationKey(subID, rg, svc, apiID, opID string) string {
	return subID + "/" + rg + "/" + svc + "/" + apiID + "/" + opID
}

func operationResourceID(subID, rg, svc, apiID, opID string) string {
	return fmt.Sprintf("%s/operations/%s", apiResourceID(subID, rg, svc, apiID), opID)
}

func (s *Service) putAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")

	if exists, err := s.serviceExists(subID, rg, svcName); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de API Management '%s' no existe en el resource group '%s'", svcName, rg))
		return
	}

	var req apiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.DisplayName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.displayName' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.Path) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.path' es obligatorio")
		return
	}

	protocols := req.Properties.Protocols
	if len(protocols) == 0 {
		protocols = []string{"https"}
	}
	apiType := req.Properties.APIType
	if apiType == "" {
		apiType = "http"
	}

	key := apiKey(subID, rg, svcName, apiID)
	var existing Api
	found, err := s.db.Get(apisBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	api := Api{
		ID:   apiResourceID(subID, rg, svcName, apiID),
		Name: apiID,
		Type: "Microsoft.ApiManagement/service/apis",
		Properties: ApiProperties{
			DisplayName:          req.Properties.DisplayName,
			Description:          req.Properties.Description,
			Path:                 req.Properties.Path,
			Protocols:            protocols,
			ServiceURL:           req.Properties.ServiceURL,
			APIType:              apiType,
			SubscriptionRequired: req.Properties.SubscriptionRequired,
			IsCurrent:            true,
		},
	}

	if err := s.db.Put(apisBucket, key, api); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, api)
}

func (s *Service) getAPIHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")

	api, found, err := s.getAPI(subID, rg, svcName, apiID)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la API '%s' no existe en el servicio '%s'", apiID, svcName))
		return
	}
	server.WriteJSON(w, http.StatusOK, api)
}

func (s *Service) listAPIs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")

	apis := make([]Api, 0)
	err := s.db.List(apisBucket, subID+"/"+rg+"/"+svcName+"/", func(key string, raw []byte) error {
		var a Api
		if err := json.Unmarshal(raw, &a); err != nil {
			return err
		}
		apis = append(apis, a)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": apis})
}

func (s *Service) deleteAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")

	if err := s.deleteOperationsForAPI(subID, rg, svcName, apiID); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Delete(apisBucket, apiKey(subID, rg, svcName, apiID)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) putOperation(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")
	opID := r.PathValue("operationId")

	if _, found, err := s.getAPI(subID, rg, svcName, apiID); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la API '%s' no existe en el servicio '%s'", apiID, svcName))
		return
	}

	var req apiOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.DisplayName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.displayName' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.Method) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.method' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.URLTemplate) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.urlTemplate' es obligatorio")
		return
	}

	key := operationKey(subID, rg, svcName, apiID, opID)
	var existing ApiOperation
	found, err := s.db.Get(operationsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	op := ApiOperation{
		ID:   operationResourceID(subID, rg, svcName, apiID, opID),
		Name: opID,
		Type: "Microsoft.ApiManagement/service/apis/operations",
		Properties: ApiOperationProperties{
			DisplayName: req.Properties.DisplayName,
			Method:      strings.ToUpper(req.Properties.Method),
			URLTemplate: req.Properties.URLTemplate,
			Description: req.Properties.Description,
		},
	}

	if err := s.db.Put(operationsBucket, key, op); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, op)
}

func (s *Service) getOperationHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")
	opID := r.PathValue("operationId")

	var op ApiOperation
	found, err := s.db.Get(operationsBucket, operationKey(subID, rg, svcName, apiID, opID), &op)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la operación '%s' no existe en la API '%s'", opID, apiID))
		return
	}
	server.WriteJSON(w, http.StatusOK, op)
}

func (s *Service) listOperations(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")

	ops := make([]ApiOperation, 0)
	prefix := subID + "/" + rg + "/" + svcName + "/" + apiID + "/"
	err := s.db.List(operationsBucket, prefix, func(key string, raw []byte) error {
		var op ApiOperation
		if err := json.Unmarshal(raw, &op); err != nil {
			return err
		}
		ops = append(ops, op)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": ops})
}

func (s *Service) deleteOperation(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	apiID := r.PathValue("apiId")
	opID := r.PathValue("operationId")

	if err := s.db.Delete(operationsBucket, operationKey(subID, rg, svcName, apiID, opID)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) getAPI(subID, rg, svc, apiID string) (Api, bool, error) {
	var api Api
	found, err := s.db.Get(apisBucket, apiKey(subID, rg, svc, apiID), &api)
	return api, found, err
}

func (s *Service) deleteOperationsForAPI(subID, rg, svc, apiID string) error {
	prefix := subID + "/" + rg + "/" + svc + "/" + apiID + "/"
	var keys []string
	if err := s.db.List(operationsBucket, prefix, func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.db.Delete(operationsBucket, k); err != nil {
			return err
		}
	}
	return nil
}

// deleteAllAPIsForService borra todas las APIs (y sus operaciones) de un
// servicio APIM, usado por service.go's deleteService al borrar la
// instancia padre en cascada.
func (s *Service) deleteAllAPIsForService(subID, rg, svc string) error {
	prefix := subID + "/" + rg + "/" + svc + "/"
	var apiIDs []string
	if err := s.db.List(apisBucket, prefix, func(key string, _ []byte) error {
		apiIDs = append(apiIDs, strings.TrimPrefix(key, prefix))
		return nil
	}); err != nil {
		return err
	}
	for _, apiID := range apiIDs {
		if err := s.deleteOperationsForAPI(subID, rg, svc, apiID); err != nil {
			return err
		}
		if err := s.db.Delete(apisBucket, apiKey(subID, rg, svc, apiID)); err != nil {
			return err
		}
	}
	return nil
}
