// Package signalr implementa Microsoft.SignalRService/signalR (ARM CRUD
// sync). Devuelve un hostName, publicPort y serverPort deterministas para
// que azurerm_signalr_service no muestre drift en terraform plan.
package signalr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	svcBucket = "signalr.services"
	svcBase   = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.SignalRService/signalR"
	svcType   = "Microsoft.SignalRService/signalR"
)

// Service expone las rutas ARM para Azure SignalR Service.
type Service struct{ db *storage.DB }

// New crea una instancia del servicio.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register registra todas las rutas ARM.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+svcBase, s.listServices)
	mux.HandleFunc("PUT "+svcBase+"/{resourceName}", s.putService)
	mux.HandleFunc("GET "+svcBase+"/{resourceName}", s.getService)
	mux.HandleFunc("DELETE "+svcBase+"/{resourceName}", s.deleteService)
	// listKeys: azurerm_signalr_service y los SDKs de cliente lo llaman.
	mux.HandleFunc("POST "+svcBase+"/{resourceName}/listKeys", s.listKeys)
}

// SignalRService replica el shape que azurerm_signalr_service lee.
type SignalRService struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        SignalRSku        `json:"sku"`
	Properties SignalRProperties `json:"properties"`
}

// SignalRSku replica la SKU de SignalR Service.
type SignalRSku struct {
	Name     string `json:"name"`
	Tier     string `json:"tier,omitempty"`
	Capacity int    `json:"capacity"`
}

// SignalRProperties incluye los campos que azurerm_signalr_service lee en Refresh.
type SignalRProperties struct {
	ProvisioningState  string              `json:"provisioningState"`
	ExternalIP         string              `json:"externalIP,omitempty"`
	HostName           string              `json:"hostName"`
	PublicPort         int                 `json:"publicPort"`
	ServerPort         int                 `json:"serverPort"`
	Version            string              `json:"version"`
	Features           []SignalRFeature    `json:"features,omitempty"`
	Cors               *SignalRCors        `json:"cors,omitempty"`
	ServiceMode        string              `json:"serviceMode,omitempty"`
}

// SignalRFeature permite configurar características como ServiceMode.
type SignalRFeature struct {
	Flag       string `json:"flag"`
	Value      string `json:"value"`
	Properties map[string]string `json:"properties,omitempty"`
}

// SignalRCors permite listas de allowed origins.
type SignalRCors struct {
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
}

type svcRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *SignalRSku       `json:"sku,omitempty"`
	Properties struct {
		Features    []SignalRFeature `json:"features,omitempty"`
		Cors        *SignalRCors     `json:"cors,omitempty"`
		ServiceMode string           `json:"serviceMode,omitempty"`
	} `json:"properties"`
}

func svcKey(sub, rg, name string) string { return sub + "/" + rg + "/" + name }
func svcID(sub, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.SignalRService/signalR/%s", sub, rg, name)
}

func fakeHostName(name string) string {
	return strings.ToLower(name) + ".service.signalr.net"
}

// skuTier deriva el tier del nombre de SKU de SignalR.
func skuTier(name string) string {
	switch strings.ToLower(name) {
	case "free_f1":
		return "Free"
	case "standard_s1":
		return "Standard"
	case "premium_p1", "premium_p2":
		return "Premium"
	default:
		return "Standard"
	}
}

func (s *Service) putService(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("resourceName")

	var req svcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "'location' es obligatorio")
		return
	}

	key := svcKey(sub, rg, name)
	var existing SignalRService
	found, _ := s.db.Get(svcBucket, key, &existing)

	sku := SignalRSku{Name: "Free_F1", Tier: "Free", Capacity: 1}
	if req.Sku != nil && req.Sku.Name != "" {
		sku = *req.Sku
		if sku.Tier == "" {
			sku.Tier = skuTier(sku.Name)
		}
		if sku.Capacity == 0 {
			sku.Capacity = 1
		}
	}

	serviceMode := req.Properties.ServiceMode
	if serviceMode == "" {
		// Extraer serviceMode de features si viene en ese formato.
		for _, f := range req.Properties.Features {
			if f.Flag == "ServiceMode" {
				serviceMode = f.Value
				break
			}
		}
	}
	if serviceMode == "" {
		serviceMode = "Default"
	}

	svc := SignalRService{
		ID:       svcID(sub, rg, name),
		Name:     name,
		Type:     svcType,
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: SignalRProperties{
			ProvisioningState: "Succeeded",
			HostName:          fakeHostName(name),
			PublicPort:        443,
			ServerPort:        443,
			Version:           "1.0",
			Features:          req.Properties.Features,
			Cors:              req.Properties.Cors,
			ServiceMode:       serviceMode,
		},
	}
	if err := s.db.Put(svcBucket, key, svc); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, svc)
}

func (s *Service) getService(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("resourceName")

	var svc SignalRService
	found, err := s.db.Get(svcBucket, svcKey(sub, rg, name), &svc)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("SignalR service '%s' no encontrado", name))
		return
	}
	server.WriteJSON(w, http.StatusOK, svc)
}

func (s *Service) listServices(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	items := make([]SignalRService, 0)
	_ = s.db.List(svcBucket, sub+"/"+rg+"/", func(_ string, raw []byte) error {
		var svc SignalRService
		if err := json.Unmarshal(raw, &svc); err == nil {
			items = append(items, svc)
		}
		return nil
	})
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": items})
}

func (s *Service) deleteService(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	sub := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("resourceName")
	_ = s.db.Delete(svcBucket, svcKey(sub, rg, name))
	w.WriteHeader(http.StatusNoContent)
}

// listKeys devuelve primary/secondary connection strings y access keys
// deterministas. azurerm_signalr_service los muestra como sensitive outputs.
func (s *Service) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	name := r.PathValue("resourceName")
	hostName := fakeHostName(name)

	fakeKey := fmt.Sprintf("fakesignalrkey%s00000000000000000000000000000000", name)
	if len(fakeKey) > 44 {
		fakeKey = fakeKey[:44]
	}
	primary := fmt.Sprintf("Endpoint=https://%s;AccessKey=%s=;Version=1.0;", hostName, fakeKey)
	secondary := fmt.Sprintf("Endpoint=https://%s;AccessKey=%ssec=;Version=1.0;", hostName, fakeKey)

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"primaryKey":                  fakeKey + "=",
		"secondaryKey":                fakeKey + "sec=",
		"primaryConnectionString":     primary,
		"secondaryConnectionString":   secondary,
	})
}
