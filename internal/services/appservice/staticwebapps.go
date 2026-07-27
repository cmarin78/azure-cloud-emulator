package appservice

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const staticSitesBucket = "appservice.staticSites"

// StaticSite es la representación ARM de Microsoft.Web/staticSites.
// El provider azurerm_static_site lee: id, name, default_host_name, api_key.
type StaticSite struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Sku        StaticSiteSku       `json:"sku,omitempty"`
	Properties StaticSiteProperties `json:"properties"`
}

// StaticSiteSku replica el objeto "sku" de staticSites
// (Free o Standard; el provider default es Free).
type StaticSiteSku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// StaticSiteProperties son los campos que azurerm_static_site lee en Read.
// defaultHostname siempre está poblado (nombre determinístico).
// contentDistributionEndpoint y repositoryUrl son opcionales en el emulador.
type StaticSiteProperties struct {
	DefaultHostname string `json:"defaultHostname"`
	// repositoryUrl/branch se aceptan pero no se validan
	RepositoryURL   string `json:"repositoryUrl,omitempty"`
	Branch          string `json:"branch,omitempty"`
}

// StaticSiteSecrets es la respuesta de
// POST .../staticSites/{name}/listSecrets — el proveedor
// azurerm_static_site necesita api_key del campo "apiKey".
type StaticSiteSecrets struct {
	Properties StaticSiteSecretsProperties `json:"properties"`
}

type StaticSiteSecretsProperties struct {
	// apiKey es el único campo que lee azurerm_static_site.
	APIKey string `json:"apiKey"`
}

type staticSiteRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *StaticSiteSku    `json:"sku,omitempty"`
	Properties struct {
		RepositoryURL string `json:"repositoryUrl,omitempty"`
		Branch        string `json:"branch,omitempty"`
	} `json:"properties,omitempty"`
}

func staticSiteKey(subID, rg, name string) string { return subID + "/" + rg + "/" + name }
func staticSiteKeyPrefix(subID, rg string) string { return subID + "/" + rg + "/" }

func staticSiteID(subID, rg, name string) string {
	return "/subscriptions/" + subID +
		"/resourceGroups/" + rg +
		"/providers/Microsoft.Web/staticSites/" + name
}

// defaultHostname genera un hostname determinístico idéntico al formato
// que usa Azure real: {name}.{azurestaticapps.net}.
func defaultHostname(name string) string {
	return name + ".azurestaticapps.net"
}

// apiKey genera una clave determinística para el static site.
// En Azure real esta es una clave secreta de ~88 chars; aquí usamos
// una cadena fake estable derivada del nombre para que los planes de
// Terraform no vean drift entre runs.
func staticSiteAPIKey(name string) string {
	// 64 hex chars fake key, prefixed con el nombre para ser único por site.
	return fmt.Sprintf("fake-api-key-for-%s-0000000000000000000000000000000000000000", name)
}

func (s *Service) registerStaticWebApps(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}" +
		"/providers/Microsoft.Web/staticSites"

	mux.HandleFunc("GET "+base, s.listStaticSites)
	mux.HandleFunc("PUT "+base+"/{staticSiteName}", s.putStaticSite)
	mux.HandleFunc("GET "+base+"/{staticSiteName}", s.getStaticSiteHandler)
	mux.HandleFunc("DELETE "+base+"/{staticSiteName}", s.deleteStaticSite)
	mux.HandleFunc("POST "+base+"/{staticSiteName}/listSecrets", s.listStaticSiteSecrets)
}

func (s *Service) putStaticSite(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("staticSiteName")

	var req staticSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	sku := StaticSiteSku{Name: "Free", Tier: "Free"}
	if req.Sku != nil && req.Sku.Name != "" {
		sku = *req.Sku
		if sku.Tier == "" {
			sku.Tier = sku.Name
		}
	}

	key := staticSiteKey(subID, rg, name)
	var existing StaticSite
	existedBefore, err := s.db.Get(staticSitesBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	site := StaticSite{
		ID:       staticSiteID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Web/staticSites",
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: StaticSiteProperties{
			DefaultHostname: defaultHostname(name),
			RepositoryURL:   req.Properties.RepositoryURL,
			Branch:          req.Properties.Branch,
		},
	}

	if err := s.db.Put(staticSitesBucket, key, site); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, site)
}

func (s *Service) getStaticSite(subID, rg, name string) (StaticSite, bool, error) {
	var site StaticSite
	found, err := s.db.Get(staticSitesBucket, staticSiteKey(subID, rg, name), &site)
	return site, found, err
}

func (s *Service) getStaticSiteHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("staticSiteName")

	site, found, err := s.getStaticSite(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el static site '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, site)
}

func (s *Service) listStaticSites(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	sites := make([]StaticSite, 0)
	err := s.db.List(staticSitesBucket, staticSiteKeyPrefix(subID, rg), func(_ string, raw []byte) error {
		var site StaticSite
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

func (s *Service) deleteStaticSite(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("staticSiteName")
	key := staticSiteKey(subID, rg, name)

	found, err := s.db.Get(staticSitesBucket, key, &StaticSite{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(staticSitesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listStaticSiteSecrets es POST .../staticSites/{name}/listSecrets.
// El provider azurerm_static_site llama a esta acción en Read para
// obtener api_key — devolvemos una clave determinística fake.
func (s *Service) listStaticSiteSecrets(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("staticSiteName")

	_, found, err := s.getStaticSite(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el static site '%s' no existe", name))
		return
	}

	server.WriteJSON(w, http.StatusOK, StaticSiteSecrets{
		Properties: StaticSiteSecretsProperties{
			APIKey: staticSiteAPIKey(name),
		},
	})
}
