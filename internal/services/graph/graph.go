// Package graph emula el subconjunto de Microsoft Graph que Entra ID/RBAC
// necesita para que tanto el flujo de autenticación existente (descubrir el
// object ID de un service principal a partir de su appId, usado por
// azurerm) como Fase 15 (app registrations + service principals explícitos
// para azuread_application/azuread_service_principal o `az ad app
// create`/`az ad sp create-for-rbac`) funcionen extremo a extremo.
//
// Sigue sin haber ningún directorio real: cualquier cuerpo de solicitud se
// acepta como válido, no se valida que displayName sea único, y appId/object
// ID se generan localmente (GUIDs aleatorios para application/objectId de
// service principal explícito, o un hash determinista del appId para el
// descubrimiento "automático" que ya hacía este paquete antes de Fase 15 --
// ver fakeObjectID más abajo). El documento de metadata de ARM
// (internal/services/armmeta) sigue apuntando "graph"/"microsoftGraphResourceId"
// de vuelta a este mismo proceso, así que azurerm/azuread terminan llamando
// aquí en vez de a graph.microsoft.com.
package graph

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	applicationsBucket      = "graph.applications"
	servicePrincipalsBucket = "graph.serviceprincipals"
)

// Service agrupa el estado necesario para atender las rutas de Microsoft
// Graph: la base de datos embebida, usada para persistir application/
// servicePrincipal creados explícitamente (Fase 15) -- el descubrimiento
// "automático" por $filter sigue sin necesitar persistencia, igual que antes.
type Service struct {
	db *storage.DB
}

func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// application replica el subconjunto mínimo de la forma real de Microsoft
// Graph que azuread_application/`az ad app` necesitan.
type application struct {
	ID             string `json:"id"`
	AppID          string `json:"appId"`
	DisplayName    string `json:"displayName"`
	SignInAudience string `json:"signInAudience,omitempty"`
}

type applicationRequest struct {
	DisplayName    string `json:"displayName"`
	SignInAudience string `json:"signInAudience,omitempty"`
}

// servicePrincipal replica el subconjunto mínimo de la forma real de
// Microsoft Graph que azurerm/azuread leen de la respuesta.
type servicePrincipal struct {
	ID          string `json:"id"`
	AppID       string `json:"appId"`
	DisplayName string `json:"displayName"`
}

type servicePrincipalRequest struct {
	AppID       string `json:"appId"`
	DisplayName string `json:"displayName,omitempty"`
}

// Register monta las rutas de Graph que emulamos. Se registran como
// prefijos literales bajo "/v1.0/" (no wildcard) para que sean
// estrictamente más específicas que el dispatcher de data-plane compartido
// ("/{accountResource}/{path...}" en cmd/azure-emulator/main.go) -- el mismo
// conflicto de net/http.ServeMux que ya se resolvió para
// internal/services/aadtoken con el prefijo "/login/".
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1.0/applications", s.createApplication)
	mux.HandleFunc("GET /v1.0/applications/{id}", s.getApplication)
	mux.HandleFunc("DELETE /v1.0/applications/{id}", s.deleteApplication)
	mux.HandleFunc("GET /v1.0/applications", s.listApplications)

	mux.HandleFunc("POST /v1.0/servicePrincipals", s.createServicePrincipal)
	mux.HandleFunc("GET /v1.0/servicePrincipals/{id}", s.getServicePrincipalByID)
	mux.HandleFunc("DELETE /v1.0/servicePrincipals/{id}", s.deleteServicePrincipal)
	mux.HandleFunc("GET /v1.0/servicePrincipals", s.listServicePrincipals)
}

// --- applications -----------------------------------------------------

func (s *Service) createApplication(w http.ResponseWriter, r *http.Request) {
	var req applicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'displayName' es obligatorio para crear una application")
		return
	}

	signInAudience := req.SignInAudience
	if signInAudience == "" {
		signInAudience = "AzureADMyOrg"
	}

	id, err := newGUID()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	appID, err := newGUID()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	app := application{
		ID:             id,
		AppID:          appID,
		DisplayName:    req.DisplayName,
		SignInAudience: signInAudience,
	}
	if err := s.db.Put(applicationsBucket, id, app); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, app)
}

func (s *Service) getApplication(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var app application
	found, err := s.db.Get(applicationsBucket, id, &app)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "Request_ResourceNotFound",
			fmt.Sprintf("no existe ninguna application con id '%s'", id))
		return
	}
	server.WriteJSON(w, http.StatusOK, app)
}

func (s *Service) listApplications(w http.ResponseWriter, r *http.Request) {
	appID := extractAppIDFilter(r.URL.Query().Get("$filter"))

	apps := make([]application, 0)
	err := s.db.List(applicationsBucket, "", func(key string, raw []byte) error {
		var app application
		if err := json.Unmarshal(raw, &app); err != nil {
			return err
		}
		if appID == "" || app.AppID == appID {
			apps = append(apps, app)
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": apps})
}

// deleteApplication es idempotente (204 si no existe), mismo patrón que el
// resto del proyecto (ver actionGroups/deleteVault).
func (s *Service) deleteApplication(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found, err := s.db.Get(applicationsBucket, id, &application{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(applicationsBucket, id); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- service principals ------------------------------------------------

// createServicePrincipal usa el mismo object ID determinista
// (fakeObjectID(appId)) que el descubrimiento "automático" vía $filter, así
// que crear explícitamente un SP para un appId y luego descubrirlo por
// filtro (lo que hace azurerm al autenticar) devuelven el mismo ID.
func (s *Service) createServicePrincipal(w http.ResponseWriter, r *http.Request) {
	var req servicePrincipalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.AppID) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'appId' es obligatorio para crear un service principal")
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = "azure-emulator fake service principal"
	}

	sp := servicePrincipal{
		ID:          fakeObjectID(req.AppID),
		AppID:       req.AppID,
		DisplayName: displayName,
	}
	if err := s.db.Put(servicePrincipalsBucket, sp.ID, sp); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, sp)
}

func (s *Service) getServicePrincipalByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var sp servicePrincipal
	found, err := s.db.Get(servicePrincipalsBucket, id, &sp)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "Request_ResourceNotFound",
			fmt.Sprintf("no existe ningún service principal con id '%s'", id))
		return
	}
	server.WriteJSON(w, http.StatusOK, sp)
}

// deleteServicePrincipal es idempotente (204 si no existe).
func (s *Service) deleteServicePrincipal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found, err := s.db.Get(servicePrincipalsBucket, id, &servicePrincipal{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(servicePrincipalsBucket, id); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listServicePrincipals interpreta el único shape de "$filter" que los
// clientes reales envían: appId eq '{valor}'. Si el SP para ese appId fue
// creado explícitamente (createServicePrincipal) se devuelve la versión
// persistida (con su displayName real); si no, se "auto-vivifica" un SP
// falso sin persistirlo -- mismo comportamiento que tenía este paquete antes
// de Fase 15, necesario para que azurerm descubra el object ID del cliente
// autenticado sin que nadie haya llamado a createServicePrincipal primero.
// Sin $filter, lista todos los SPs creados explícitamente (puede ser vacío).
func (s *Service) listServicePrincipals(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("$filter")
	if filter == "" {
		sps := make([]servicePrincipal, 0)
		err := s.db.List(servicePrincipalsBucket, "", func(key string, raw []byte) error {
			var sp servicePrincipal
			if err := json.Unmarshal(raw, &sp); err != nil {
				return err
			}
			sps = append(sps, sp)
			return nil
		})
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		server.WriteJSON(w, http.StatusOK, map[string]any{"value": sps})
		return
	}

	appID := extractAppIDFilter(filter)
	if appID == "" {
		server.WriteJSON(w, http.StatusOK, map[string]any{"value": []servicePrincipal{}})
		return
	}

	id := fakeObjectID(appID)
	var sp servicePrincipal
	found, err := s.db.Get(servicePrincipalsBucket, id, &sp)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		sp = servicePrincipal{
			ID:          id,
			AppID:       appID,
			DisplayName: "azure-emulator fake service principal",
		}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []servicePrincipal{sp}})
}

// extractAppIDFilter interpreta el único shape de "$filter" que azurerm
// envía: appId eq '{valor}' (con comillas simples). No es un parser
// general de OData -- basta con lo que el cliente real manda.
func extractAppIDFilter(filter string) string {
	const prefix = "appId eq '"
	if len(filter) <= len(prefix)+1 || filter[:len(prefix)] != prefix {
		return ""
	}
	rest := filter[len(prefix):]
	end := -1
	for i, c := range rest {
		if c == '\'' {
			end = i
			break
		}
	}
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// fakeObjectID deriva un GUID estable a partir del appId, para que el mismo
// service principal "falso" siempre tenga el mismo object ID entre
// reinicios del emulador (sin necesidad de persistirlo en BoltDB) cuando se
// descubre por $filter en vez de crearse explícitamente.
func fakeObjectID(appID string) string {
	sum := sha256.Sum256([]byte("azure-emulator-fake-object-id:" + appID))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// newGUID genera un GUID aleatorio (sin los bits de versión/variante del
// estándar -- no hace falta, solo unicidad práctica) para IDs de recursos
// creados explícitamente (applications/service principals), donde a
// diferencia del descubrimiento por $filter sí persistimos el valor en
// BoltDB y no hace falta que sea determinista.
func newGUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("graph: error generando id aleatorio: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}
