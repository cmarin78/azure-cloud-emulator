package appservice

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const sitesBucket = "appservice.sites"
const appSettingsBucket = "appservice.appsettings"

// SiteConfig replica el subconjunto de "properties.siteConfig" que
// az/Terraform leen al crear o consultar un site. Las app settings NO viven
// aquí: Azure real las gestiona como un sub-recurso aparte
// (Microsoft.Web/sites/config/appsettings, un StringDictionary plano), que
// es exactamente como azurerm_linux_web_app/azurerm_windows_web_app las
// escriben (ver registerAppSettings más abajo) -- por eso GET de un site no
// las incluye salvo que se pidan explícitamente vía ese sub-recurso.
type SiteConfig struct {
	LinuxFxVersion string `json:"linuxFxVersion,omitempty"`
	AlwaysOn       bool   `json:"alwaysOn"`
}

// Site replica el subconjunto relevante de Microsoft.Web/sites.
type Site struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Identity   *Identity         `json:"identity,omitempty"`
	Properties SiteProperties    `json:"properties"`
}

// Identity replica el bloque "identity" estándar de ARM (Phase 16, mismo
// shape que aks.Identity/compute.Identity — cada paquete mantiene su propia
// copia local en vez de compartir un tipo central, igual convención que
// fakeHexSuffix/fakeGUID más abajo). Function Apps (Phase 14) reutilizan
// este mismo Site, así que heredan soporte de identity sin cambios.
type Identity struct {
	Type        string `json:"type"`
	PrincipalID string `json:"principalId,omitempty"`
	TenantID    string `json:"tenantId,omitempty"`
}

type SiteProperties struct {
	ProvisioningState string     `json:"provisioningState"`
	ServerFarmID      string     `json:"serverFarmId"`
	State             string     `json:"state"`
	Enabled           bool       `json:"enabled"`
	HTTPSOnly         bool       `json:"httpsOnly"`
	DefaultHostName   string     `json:"defaultHostName"`
	SiteConfig        SiteConfig `json:"siteConfig"`
}

type siteRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Kind       string            `json:"kind"`
	Identity   *Identity         `json:"identity,omitempty"`
	Properties struct {
		ServerFarmID string `json:"serverFarmId"`
		HTTPSOnly    *bool  `json:"httpsOnly"`
		Enabled      *bool  `json:"enabled"`
		SiteConfig   struct {
			LinuxFxVersion string `json:"linuxFxVersion"`
			AlwaysOn       *bool  `json:"alwaysOn"`
		} `json:"siteConfig"`
	} `json:"properties"`
}

func siteKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func siteID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s", subID, rg, name)
}

// defaultHostName deriva el hostname público falso de un site, igual de
// determinista que monitor.fakeCustomerID: siempre el mismo valor para el
// mismo nombre, sin necesidad de un registro DNS real detrás.
func defaultHostName(name string) string {
	return strings.ToLower(name) + ".azurewebsites.net"
}

// fakeHexSuffix/fakeGUID derivan valores deterministas a partir del ID
// completo del recurso, mismo patrón que aks.fakeHexSuffix/fakeGUID
// (Phase 13) y compute.fakeHexSuffix/fakeGUID (Phase 16) — cada paquete
// mantiene su propia copia local en vez de un helper compartido.
func fakeHexSuffix(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

func fakeGUID(seed string) string {
	sum := fakeHexSuffix(seed)
	return fmt.Sprintf("%08x-0000-0000-0000-%012x", sum, uint64(sum)*2654435761)
}

func (s *Service) registerSites(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Web/sites"
	mux.HandleFunc("GET "+base, s.listSites)
	mux.HandleFunc("PUT "+base+"/{siteName}", s.putSite)
	mux.HandleFunc("GET "+base+"/{siteName}", s.getSiteHandler)
	mux.HandleFunc("DELETE "+base+"/{siteName}", s.deleteSite)
	mux.HandleFunc("POST "+base+"/{siteName}/start", s.startSite)
	mux.HandleFunc("POST "+base+"/{siteName}/stop", s.stopSite)
	mux.HandleFunc("POST "+base+"/{siteName}/restart", s.restartSite)
	mux.HandleFunc("PUT "+base+"/{siteName}/config/appsettings", s.putAppSettings)
	mux.HandleFunc("GET "+base+"/{siteName}/config/appsettings", s.getAppSettings)
	mux.HandleFunc("POST "+base+"/{siteName}/config/appsettings/list", s.getAppSettings)
}

// putSite es síncrono, mismo Effort "S" que putPlan: no valida que
// serverFarmId apunte a un plan existente (mismo enfoque "sin integridad
// referencial estricta" que metricAlerts/actionGroupId en monitor), solo
// que el campo no venga vacío.
func (s *Service) putSite(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")

	var req siteRequest
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
	if strings.TrimSpace(req.Properties.ServerFarmID) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.serverFarmId' es obligatorio")
		return
	}

	httpsOnly := false
	if req.Properties.HTTPSOnly != nil {
		httpsOnly = *req.Properties.HTTPSOnly
	}
	enabled := true
	if req.Properties.Enabled != nil {
		enabled = *req.Properties.Enabled
	}
	alwaysOn := false
	if req.Properties.SiteConfig.AlwaysOn != nil {
		alwaysOn = *req.Properties.SiteConfig.AlwaysOn
	}

	key := siteKey(subID, rg, name)
	existing, found, err := s.getSite(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	state := "Running"
	if found {
		state = existing.Properties.State
	}

	id := siteID(subID, rg, name)
	var identity *Identity
	if req.Identity != nil {
		identity = &Identity{Type: req.Identity.Type}
		if req.Identity.Type != "" && req.Identity.Type != "None" {
			identity.PrincipalID = fakeGUID(id + "-principal")
			identity.TenantID = fakeGUID(id + "-tenant")
		}
	}

	site := Site{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.Web/sites",
		Location: req.Location,
		Tags:     req.Tags,
		Kind:     req.Kind,
		Identity: identity,
		Properties: SiteProperties{
			ProvisioningState: "Succeeded",
			ServerFarmID:      req.Properties.ServerFarmID,
			State:             state,
			Enabled:           enabled,
			HTTPSOnly:         httpsOnly,
			DefaultHostName:   defaultHostName(name),
			SiteConfig: SiteConfig{
				LinuxFxVersion: req.Properties.SiteConfig.LinuxFxVersion,
				AlwaysOn:       alwaysOn,
			},
		},
	}

	if err := s.db.Put(sitesBucket, key, site); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, site)
}

func (s *Service) getSite(subID, rg, name string) (Site, bool, error) {
	var site Site
	found, err := s.db.Get(sitesBucket, siteKey(subID, rg, name), &site)
	return site, found, err
}

func (s *Service) getSiteHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")

	site, found, err := s.getSite(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el site '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, site)
}

func (s *Service) listSites(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	sites := make([]Site, 0)
	err := s.db.List(sitesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var site Site
		if err := json.Unmarshal(raw, &site); err != nil {
			return err
		}
		sites = append(sites, site)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": sites})
}

// deleteSite es idempotente (204 si no existe) y síncrono, igual que
// deletePlan/deleteVault. También limpia el bucket de app settings del
// site, ya que en Azure real es un sub-recurso anidado que se borra en
// cascada junto con el site.
func (s *Service) deleteSite(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")
	key := siteKey(subID, rg, name)

	found, err := s.db.Get(sitesBucket, key, &Site{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(sitesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	_ = s.db.Delete(appSettingsBucket, key)
	w.WriteHeader(http.StatusOK)
}

// startSite, stopSite y restartSite emulan las acciones POST .../start,
// .../stop y .../restart que az CLI (az webapp start/stop/restart) usa
// sobre un site ya creado. A diferencia de compute.startVirtualMachine/
// powerOffVirtualMachine (que son LRO), aquí son síncronas porque el
// Effort de este paquete es "S" (igual que workspaceSharedKeys en monitor):
// no hace falta emular polling para que az CLI/Terraform completen el flujo.
func (s *Service) startSite(w http.ResponseWriter, r *http.Request) {
	s.setSiteState(w, r, "Running")
}

func (s *Service) stopSite(w http.ResponseWriter, r *http.Request) {
	s.setSiteState(w, r, "Stopped")
}

func (s *Service) restartSite(w http.ResponseWriter, r *http.Request) {
	s.setSiteState(w, r, "Running")
}

func (s *Service) setSiteState(w http.ResponseWriter, r *http.Request, state string) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")
	key := siteKey(subID, rg, name)

	site, found, err := s.getSite(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el site '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	site.Properties.State = state
	if err := s.db.Put(sitesBucket, key, site); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// appSettingsRequest replica el "StringDictionary" que Azure real usa para
// Microsoft.Web/sites/config/appsettings: un PUT con
// {"properties": {"KEY": "value", ...}} reemplaza el diccionario completo
// (no hace merge), igual que azurerm_linux_web_app/azurerm_windows_web_app
// lo escriben al aplicar el bloque app_settings.
type appSettingsRequest struct {
	Properties map[string]string `json:"properties"`
}

// putAppSettings reemplaza el StringDictionary completo del site. No
// valida que el site exista primero -- igual que Azure real, que
// "auto-crea" el sub-recurso de config la primera vez que se escribe.
func (s *Service) putAppSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")

	var req appSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if req.Properties == nil {
		req.Properties = map[string]string{}
	}

	key := siteKey(subID, rg, name)
	if err := s.db.Put(appSettingsBucket, key, req.Properties); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	s.writeAppSettings(w, name, req.Properties)
}

func (s *Service) getAppSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("siteName")

	props := map[string]string{}
	_, err := s.db.Get(appSettingsBucket, siteKey(subID, rg, name), &props)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	s.writeAppSettings(w, name, props)
}

func (s *Service) writeAppSettings(w http.ResponseWriter, name string, props map[string]string) {
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"id":         name + "/config/appsettings",
		"name":       "appsettings",
		"type":       "Microsoft.Web/sites/config",
		"properties": props,
	})
}
