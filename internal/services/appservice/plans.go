package appservice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const plansBucket = "appservice.plans"

// Sku replica "sku" de un App Service Plan (p. ej.
// {"name": "B1", "tier": "Basic", "size": "B1", "family": "B", "capacity": 1}).
// A diferencia de monitor.Sku (un solo campo), aquí el shape real de Azure
// trae varios campos redundantes entre sí (tier/size/family derivan todos
// del mismo nombre de SKU); como ningún cliente de prueba de este proyecto
// depende de que se infieran automáticamente, se persisten tal cual los
// envía el request.
type Sku struct {
	Name     string `json:"name"`
	Tier     string `json:"tier,omitempty"`
	Size     string `json:"size,omitempty"`
	Family   string `json:"family,omitempty"`
	Capacity int    `json:"capacity,omitempty"`
}

// AppServicePlan replica el subconjunto relevante de
// Microsoft.Web/serverfarms que az/Terraform (azurerm_service_plan) leen.
type AppServicePlan struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Kind       string                   `json:"kind,omitempty"`
	Sku        Sku                      `json:"sku"`
	Properties AppServicePlanProperties `json:"properties"`
}

type AppServicePlanProperties struct {
	ProvisioningState string `json:"provisioningState"`
	Reserved          bool   `json:"reserved"`
	PerSiteScaling    bool   `json:"perSiteScaling"`
	Status            string `json:"status"`
	NumberOfSites     int    `json:"numberOfSites"`
}

type planRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Kind       string            `json:"kind"`
	Sku        Sku               `json:"sku"`
	Properties struct {
		Reserved       *bool `json:"reserved"`
		PerSiteScaling *bool `json:"perSiteScaling"`
	} `json:"properties"`
}

func planKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func planID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/serverfarms/%s", subID, rg, name)
}

func (s *Service) registerPlans(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Web/serverfarms"
	mux.HandleFunc("GET "+base, s.listPlans)
	mux.HandleFunc("PUT "+base+"/{planName}", s.putPlan)
	mux.HandleFunc("GET "+base+"/{planName}", s.getPlanHandler)
	mux.HandleFunc("DELETE "+base+"/{planName}", s.deletePlan)
}

// putPlan es síncrono (Effort "S", igual que vaults/disks/VNets): crear un
// App Service Plan en Azure real no requiere polling en los flujos comunes
// de az CLI/Terraform.
func (s *Service) putPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("planName")

	var req planRequest
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

	sku := req.Sku
	if sku.Capacity == 0 {
		sku.Capacity = 1
	}

	reserved := strings.Contains(strings.ToLower(req.Kind), "linux")
	if req.Properties.Reserved != nil {
		reserved = *req.Properties.Reserved
	}
	perSiteScaling := false
	if req.Properties.PerSiteScaling != nil {
		perSiteScaling = *req.Properties.PerSiteScaling
	}

	key := planKey(subID, rg, name)
	existing, found, err := s.getPlan(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	numberOfSites := 0
	if found {
		numberOfSites = existing.Properties.NumberOfSites
	}

	plan := AppServicePlan{
		ID:       planID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Web/serverfarms",
		Location: req.Location,
		Tags:     req.Tags,
		Kind:     req.Kind,
		Sku:      sku,
		Properties: AppServicePlanProperties{
			ProvisioningState: "Succeeded",
			Reserved:          reserved,
			PerSiteScaling:    perSiteScaling,
			Status:            "Ready",
			NumberOfSites:     numberOfSites,
		},
	}

	if err := s.db.Put(plansBucket, key, plan); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, plan)
}

func (s *Service) getPlan(subID, rg, name string) (AppServicePlan, bool, error) {
	var plan AppServicePlan
	found, err := s.db.Get(plansBucket, planKey(subID, rg, name), &plan)
	return plan, found, err
}

func (s *Service) getPlanHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("planName")

	plan, found, err := s.getPlan(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el App Service Plan '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, plan)
}

func (s *Service) listPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	plans := make([]AppServicePlan, 0)
	err := s.db.List(plansBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var plan AppServicePlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return err
		}
		plans = append(plans, plan)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": plans})
}

// deletePlan es idempotente (204 si no existe) y síncrono, igual que
// deleteVault. No valida que no haya sites referenciándolo (mismo enfoque
// "sin integridad referencial estricta" que metricAlerts con su actionGroupId).
func (s *Service) deletePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("planName")
	key := planKey(subID, rg, name)

	found, err := s.db.Get(plansBucket, key, &AppServicePlan{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(plansBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
