package sql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const serversBucket = "sql.servers"

// Server replica el subconjunto relevante de Microsoft.Sql/servers que
// az/Terraform (azurerm_mssql_server) leen.
type Server struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Properties ServerProperties  `json:"properties"`
}

// ServerProperties replica "properties" de un logical server. A diferencia
// del request, no expone administratorLoginPassword -- igual que
// compute.OsProfile no expone AdminPassword en la respuesta.
type ServerProperties struct {
	AdministratorLogin       string `json:"administratorLogin"`
	Version                  string `json:"version"`
	State                    string `json:"state"`
	FullyQualifiedDomainName string `json:"fullyQualifiedDomainName"`
	PublicNetworkAccess      string `json:"publicNetworkAccess,omitempty"`
	MinimalTlsVersion        string `json:"minimalTlsVersion,omitempty"`
}

type serverRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Properties struct {
		AdministratorLogin         string `json:"administratorLogin"`
		AdministratorLoginPassword string `json:"administratorLoginPassword"`
		Version                    string `json:"version"`
		PublicNetworkAccess        string `json:"publicNetworkAccess"`
		MinimalTlsVersion          string `json:"minimalTlsVersion"`
	} `json:"properties"`
}

func serverKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func serverID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Sql/servers/%s", subID, rg, name)
}

// fakeFQDN deriva fullyQualifiedDomainName a partir del nombre del
// servidor. Usa el dominio real de Azure SQL ("database.windows.net")
// porque el nombre del server ya es único dentro del namespace global de
// Azure SQL -- no hace falta un sufijo extra/hash como en storage
// accounts (ver storageaccounts.fakeHexSuffix para ese caso).
func fakeFQDN(name string) string {
	return strings.ToLower(name) + ".database.windows.net"
}

func (s *Service) registerServers(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers"
	mux.HandleFunc("GET "+base, s.listServers)
	mux.HandleFunc("PUT "+base+"/{serverName}", s.putServer)
	mux.HandleFunc("GET "+base+"/{serverName}", s.getServerHandler)
	mux.HandleFunc("DELETE "+base+"/{serverName}", s.deleteServer)
}

// putServer es síncrono (Effort "S", igual que keyvault.Vault/
// managedidentity.UserAssignedIdentity): crear un logical server en Azure
// real no requiere polling en los flujos comunes de az CLI/Terraform.
// administratorLogin se preserva entre updates (no se permite "rotar" el
// admin login vía PUT, igual que Azure real lo trata como inmutable tras
// la creación), mismo enfoque que tenantId/principalId/clientId en
// managedidentity.
func (s *Service) putServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")

	var req serverRequest
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
	if strings.TrimSpace(req.Properties.AdministratorLogin) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.administratorLogin' es obligatorio")
		return
	}

	key := serverKey(subID, rg, name)
	existing, found, err := s.getServer(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	adminLogin := req.Properties.AdministratorLogin
	version := req.Properties.Version
	if version == "" {
		version = "12.0"
	}
	minTLS := req.Properties.MinimalTlsVersion
	if minTLS == "" {
		minTLS = "1.2"
	}
	publicAccess := req.Properties.PublicNetworkAccess
	if publicAccess == "" {
		publicAccess = "Enabled"
	}
	if found {
		adminLogin = existing.Properties.AdministratorLogin
		version = existing.Properties.Version
	}

	kind := req.Kind
	if kind == "" {
		kind = "v12.0"
	}

	srv := Server{
		ID:       serverID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Sql/servers",
		Location: req.Location,
		Tags:     req.Tags,
		Kind:     kind,
		Properties: ServerProperties{
			AdministratorLogin:       adminLogin,
			Version:                  version,
			State:                    "Ready",
			FullyQualifiedDomainName: fakeFQDN(name),
			PublicNetworkAccess:      publicAccess,
			MinimalTlsVersion:        minTLS,
		},
	}

	if err := s.db.Put(serversBucket, key, srv); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, srv)
}

func (s *Service) getServer(subID, rg, name string) (Server, bool, error) {
	var srv Server
	found, err := s.db.Get(serversBucket, serverKey(subID, rg, name), &srv)
	return srv, found, err
}

func (s *Service) getServerHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")

	srv, found, err := s.getServer(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, srv)
}

func (s *Service) listServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	servers := make([]Server, 0)
	err := s.db.List(serversBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
		var srv Server
		if err := json.Unmarshal(raw, &srv); err != nil {
			return err
		}
		servers = append(servers, srv)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": servers})
}

// deleteServer es idempotente (204 si no existe) y síncrono. No hace
// cascade-delete de databases/firewallRules anidados -- mismo enfoque "sin
// integridad referencial estricta" que managedidentity.deleteUserAssignedIdentity.
func (s *Service) deleteServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("serverName")
	key := serverKey(subID, rg, name)

	found, err := s.db.Get(serversBucket, key, &Server{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(serversBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
