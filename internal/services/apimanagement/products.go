package apimanagement

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const productsBucket = "apimanagement.products"
const productAPIsBucket = "apimanagement.productapis"
const subscriptionsBucket = "apimanagement.subscriptions"

// Product replica la forma estándar de ARM para
// Microsoft.ApiManagement/service/products. Sub-recurso síncrono, mismo
// criterio que apis.go.
type Product struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Properties ProductProperties `json:"properties"`
}

// ProductProperties replica el subconjunto más usado de properties para
// un producto de APIM.
type ProductProperties struct {
	DisplayName          string `json:"displayName"`
	Description          string `json:"description,omitempty"`
	Terms                string `json:"terms,omitempty"`
	SubscriptionRequired bool   `json:"subscriptionRequired"`
	ApprovalRequired     bool   `json:"approvalRequired"`
	State                string `json:"state"`
}

type productRequest struct {
	Properties struct {
		DisplayName          string `json:"displayName"`
		Description          string `json:"description,omitempty"`
		Terms                string `json:"terms,omitempty"`
		SubscriptionRequired *bool  `json:"subscriptionRequired,omitempty"`
		ApprovalRequired     *bool  `json:"approvalRequired,omitempty"`
		State                string `json:"state,omitempty"`
	} `json:"properties"`
}

// ApimSubscription replica la forma estándar de ARM para
// Microsoft.ApiManagement/service/subscriptions. Las llaves primaria y
// secundaria son valores falsos derivados de forma determinista del ID
// del recurso, mismo enfoque que Service Bus/Storage usan para sus
// connection strings/access keys simuladas.
type ApimSubscription struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Properties ApimSubscriptionProperties `json:"properties"`
}

// ApimSubscriptionProperties replica el subconjunto más usado de
// properties para una subscription de APIM.
type ApimSubscriptionProperties struct {
	DisplayName  string `json:"displayName"`
	Scope        string `json:"scope"`
	State        string `json:"state"`
	PrimaryKey   string `json:"primaryKey"`
	SecondaryKey string `json:"secondaryKey"`
}

type subscriptionRequest struct {
	Properties struct {
		DisplayName string `json:"displayName"`
		Scope       string `json:"scope"`
		State       string `json:"state,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerProducts(mux *http.ServeMux) {
	const svcBase = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ApiManagement/service/{serviceName}"
	const productsBase = svcBase + "/products"

	mux.HandleFunc("GET "+productsBase, s.listProducts)
	mux.HandleFunc("PUT "+productsBase+"/{productId}", s.putProduct)
	mux.HandleFunc("GET "+productsBase+"/{productId}", s.getProductHandler)
	mux.HandleFunc("DELETE "+productsBase+"/{productId}", s.deleteProduct)

	mux.HandleFunc("GET "+productsBase+"/{productId}/apis", s.listProductAPIs)
	mux.HandleFunc("PUT "+productsBase+"/{productId}/apis/{apiId}", s.putProductAPI)
	mux.HandleFunc("DELETE "+productsBase+"/{productId}/apis/{apiId}", s.deleteProductAPI)

	const subsBase = svcBase + "/subscriptions"
	mux.HandleFunc("GET "+subsBase, s.listSubscriptions)
	mux.HandleFunc("PUT "+subsBase+"/{subscriptionId2}", s.putSubscription)
	mux.HandleFunc("GET "+subsBase+"/{subscriptionId2}", s.getSubscriptionHandler)
	mux.HandleFunc("DELETE "+subsBase+"/{subscriptionId2}", s.deleteSubscription)
}

func productKey(subID, rg, svc, productID string) string {
	return subID + "/" + rg + "/" + svc + "/" + productID
}

func productResourceID(subID, rg, svc, productID string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ApiManagement/service/%s/products/%s", subID, rg, svc, productID)
}

func productAPIKey(subID, rg, svc, productID, apiID string) string {
	return subID + "/" + rg + "/" + svc + "/" + productID + "/" + apiID
}

func subscriptionKey(subID, rg, svc, subscriptionID string) string {
	return subID + "/" + rg + "/" + svc + "/" + subscriptionID
}

func subscriptionResourceID(subID, rg, svc, subscriptionID string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ApiManagement/service/%s/subscriptions/%s", subID, rg, svc, subscriptionID)
}

func (s *Service) putProduct(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")

	if exists, err := s.serviceExists(subID, rg, svcName); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de API Management '%s' no existe en el resource group '%s'", svcName, rg))
		return
	}

	var req productRequest
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

	subscriptionRequired := true
	if req.Properties.SubscriptionRequired != nil {
		subscriptionRequired = *req.Properties.SubscriptionRequired
	}
	approvalRequired := false
	if req.Properties.ApprovalRequired != nil {
		approvalRequired = *req.Properties.ApprovalRequired
	}
	state := req.Properties.State
	if state == "" {
		state = "published"
	}

	key := productKey(subID, rg, svcName, productID)
	var existing Product
	found, err := s.db.Get(productsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	product := Product{
		ID:   productResourceID(subID, rg, svcName, productID),
		Name: productID,
		Type: "Microsoft.ApiManagement/service/products",
		Properties: ProductProperties{
			DisplayName:          req.Properties.DisplayName,
			Description:          req.Properties.Description,
			Terms:                req.Properties.Terms,
			SubscriptionRequired: subscriptionRequired,
			ApprovalRequired:     approvalRequired,
			State:                state,
		},
	}

	if err := s.db.Put(productsBucket, key, product); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, product)
}

func (s *Service) getProductHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")

	var product Product
	found, err := s.db.Get(productsBucket, productKey(subID, rg, svcName, productID), &product)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el producto '%s' no existe en el servicio '%s'", productID, svcName))
		return
	}
	server.WriteJSON(w, http.StatusOK, product)
}

func (s *Service) listProducts(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")

	products := make([]Product, 0)
	err := s.db.List(productsBucket, subID+"/"+rg+"/"+svcName+"/", func(key string, raw []byte) error {
		var p Product
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		products = append(products, p)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": products})
}

func (s *Service) deleteProduct(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")

	if err := s.deleteProductAPIsForProduct(subID, rg, svcName, productID); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Delete(productsBucket, productKey(subID, rg, svcName, productID)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putProductAPI emula PUT .../products/{productId}/apis/{apiId}: la
// asociación que liga una API existente a un producto
// (azurerm_api_management_product_api). No tiene cuerpo propio en Azure
// real — es una asociación pura — así que aquí simplemente se persiste un
// marcador y se devuelve el shape de la API asociada, igual que hace
// Azure real en la respuesta de este PUT.
func (s *Service) putProductAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")
	apiID := r.PathValue("apiId")

	if _, found, err := s.getProduct(subID, rg, svcName, productID); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el producto '%s' no existe en el servicio '%s'", productID, svcName))
		return
	}
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

	if err := s.db.Put(productAPIsBucket, productAPIKey(subID, rg, svcName, productID, apiID), api); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, api)
}

func (s *Service) listProductAPIs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")

	apis := make([]Api, 0)
	prefix := subID + "/" + rg + "/" + svcName + "/" + productID + "/"
	err := s.db.List(productAPIsBucket, prefix, func(key string, raw []byte) error {
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

func (s *Service) deleteProductAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	productID := r.PathValue("productId")
	apiID := r.PathValue("apiId")

	if err := s.db.Delete(productAPIsBucket, productAPIKey(subID, rg, svcName, productID, apiID)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) getProduct(subID, rg, svc, productID string) (Product, bool, error) {
	var p Product
	found, err := s.db.Get(productsBucket, productKey(subID, rg, svc, productID), &p)
	return p, found, err
}

func (s *Service) deleteProductAPIsForProduct(subID, rg, svc, productID string) error {
	prefix := subID + "/" + rg + "/" + svc + "/" + productID + "/"
	var keys []string
	if err := s.db.List(productAPIsBucket, prefix, func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.db.Delete(productAPIsBucket, k); err != nil {
			return err
		}
	}
	return nil
}

// deleteAllProductsForService borra todos los productos (y sus
// asociaciones product-api) de un servicio APIM, usado por
// service.go's deleteService al borrar la instancia padre en cascada.
func (s *Service) deleteAllProductsForService(subID, rg, svc string) error {
	prefix := subID + "/" + rg + "/" + svc + "/"
	var productIDs []string
	if err := s.db.List(productsBucket, prefix, func(key string, _ []byte) error {
		productIDs = append(productIDs, strings.TrimPrefix(key, prefix))
		return nil
	}); err != nil {
		return err
	}
	for _, productID := range productIDs {
		if err := s.deleteProductAPIsForProduct(subID, rg, svc, productID); err != nil {
			return err
		}
		if err := s.db.Delete(productsBucket, productKey(subID, rg, svc, productID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) putSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	subscriptionID := r.PathValue("subscriptionId2")

	if exists, err := s.serviceExists(subID, rg, svcName); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de API Management '%s' no existe en el resource group '%s'", svcName, rg))
		return
	}

	var req subscriptionRequest
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
	if strings.TrimSpace(req.Properties.Scope) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.scope' es obligatorio")
		return
	}

	state := req.Properties.State
	if state == "" {
		state = "active"
	}

	id := subscriptionResourceID(subID, rg, svcName, subscriptionID)
	key := subscriptionKey(subID, rg, svcName, subscriptionID)
	var existing ApimSubscription
	found, err := s.db.Get(subscriptionsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	sub := ApimSubscription{
		ID:   id,
		Name: subscriptionID,
		Type: "Microsoft.ApiManagement/service/subscriptions",
		Properties: ApimSubscriptionProperties{
			DisplayName:  req.Properties.DisplayName,
			Scope:        req.Properties.Scope,
			State:        state,
			PrimaryKey:   fakeGUID(id + "-primary"),
			SecondaryKey: fakeGUID(id + "-secondary"),
		},
	}

	if err := s.db.Put(subscriptionsBucket, key, sub); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, sub)
}

func (s *Service) getSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	svcName := r.PathValue("serviceName")
	subscriptionID := r.PathValue("subscriptionId2")

	var sub ApimSubscription
	found, err := s.db.Get(subscriptionsBucket, subscriptionKey(subID, rg, svcName, subscriptionID), &sub)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la subscription '%s' no existe en el servicio '%s'", subscriptionID, svcName))
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
	svcName := r.PathValue("serviceName")

	subs := make([]ApimSubscription, 0)
	err := s.db.List(subscriptionsBucket, subID+"/"+rg+"/"+svcName+"/", func(key string, raw []byte) error {
		var sub ApimSubscription
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
	svcName := r.PathValue("serviceName")
	subscriptionID := r.PathValue("subscriptionId2")

	if err := s.db.Delete(subscriptionsBucket, subscriptionKey(subID, rg, svcName, subscriptionID)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteAllSubscriptionsForService borra todas las subscriptions de un
// servicio APIM, usado por service.go's deleteService al borrar la
// instancia padre en cascada.
func (s *Service) deleteAllSubscriptionsForService(subID, rg, svc string) error {
	prefix := subID + "/" + rg + "/" + svc + "/"
	var keys []string
	if err := s.db.List(subscriptionsBucket, prefix, func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.db.Delete(subscriptionsBucket, k); err != nil {
			return err
		}
	}
	return nil
}
