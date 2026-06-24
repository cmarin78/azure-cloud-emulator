package sql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const connectionPoliciesBucket = "sql.connectionpolicies"

// ConnectionPolicy replica Microsoft.Sql/servers/connectionPolicies, un
// sub-recurso singleton (siempre se llama "default", no soporta LIST/DELETE
// en Azure real). azurerm_mssql_server hace un PUT a este endpoint después
// de crear el server, incluso cuando el usuario no fija explícitamente el
// atributo "connection_policy" (tiene un default), así que el emulador
// necesita responder algo válido aquí o el provider real falla con 404.
type ConnectionPolicy struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Properties ConnectionPolicyProperties `json:"properties"`
}

type ConnectionPolicyProperties struct {
	ConnectionType string `json:"connectionType"`
}

type connectionPolicyRequest struct {
	Properties ConnectionPolicyProperties `json:"properties"`
}

func connectionPolicyKey(subID, rg, srvName, policyName string) string {
	return subID + "/" + rg + "/" + srvName + "/" + policyName
}

func (s *Service) registerConnectionPolicies(mux *http.ServeMux) {
	collection := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Sql/servers/{serverName}/connectionPolicies"
	mux.HandleFunc("PUT "+collection+"/{policyName}", s.putConnectionPolicy)
	mux.HandleFunc("GET "+collection+"/{policyName}", s.getConnectionPolicyHandler)

	// El LRO poller de go-azure-sdk para connectionPolicies (visto en
	// AzureRM Request logs de azurerm_mssql_server) no vuelve a consultar
	// el singleton .../connectionPolicies/default después del PUT --
	// consulta la colección .../connectionPolicies (sin nombre) como
	// "are we done yet?". Sin esta ruta, ese poll cae en el dispatcher de
	// data-plane y devuelve 404, lo que el provider real interpreta como
	// "la operación falló" en vez de "ya terminó".
	mux.HandleFunc("GET "+collection, s.listConnectionPolicies)
}

// putConnectionPolicy es síncrono, mismo patrón de sub-recurso anidado de
// un solo nivel que firewallRules: requiere que el server padre exista
// (404 ResourceNotFound si no). Sin "default" implícito en connectionType,
// igual que Azure real usa "Default" cuando no se especifica otra cosa.
func (s *Service) putConnectionPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	policyName := r.PathValue("policyName")

	srv, found, err := s.getServer(subID, rg, srvName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", srvName, rg))
		return
	}

	var req connectionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	connType := strings.TrimSpace(req.Properties.ConnectionType)
	if connType == "" {
		connType = "Default"
	}

	policy := ConnectionPolicy{
		ID:   srv.ID + "/connectionPolicies/" + policyName,
		Name: policyName,
		Type: "Microsoft.Sql/servers/connectionPolicies",
		Properties: ConnectionPolicyProperties{
			ConnectionType: connType,
		},
	}
	key := connectionPolicyKey(subID, rg, srvName, policyName)
	if err := s.db.Put(connectionPoliciesBucket, key, policy); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, policy)
}

// listConnectionPolicies devuelve la colección con la única policy
// ("default") como elemento, usando el mismo fallback implícito que
// getConnectionPolicyHandler cuando nunca se hizo un PUT explícito.
func (s *Service) listConnectionPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")

	srv, found, err := s.getServer(subID, rg, srvName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", srvName, rg))
		return
	}

	var policy ConnectionPolicy
	key := connectionPolicyKey(subID, rg, srvName, "default")
	policyFound, err := s.db.Get(connectionPoliciesBucket, key, &policy)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !policyFound {
		policy = ConnectionPolicy{
			ID:   srv.ID + "/connectionPolicies/default",
			Name: "default",
			Type: "Microsoft.Sql/servers/connectionPolicies",
			Properties: ConnectionPolicyProperties{
				ConnectionType: "Default",
			},
		}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": []ConnectionPolicy{policy}})
}

func (s *Service) getConnectionPolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	srvName := r.PathValue("serverName")
	policyName := r.PathValue("policyName")

	var policy ConnectionPolicy
	key := connectionPolicyKey(subID, rg, srvName, policyName)
	found, err := s.db.Get(connectionPoliciesBucket, key, &policy)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		// Mismo default implícito "Default" que devolvería Azure real
		// para un server que nunca recibió un PUT explícito a este
		// sub-recurso.
		srv, srvFound, err := s.getServer(subID, rg, srvName)
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		if !srvFound {
			server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
				fmt.Sprintf("el SQL server '%s' no existe en el resource group '%s'", srvName, rg))
			return
		}
		server.WriteJSON(w, http.StatusOK, ConnectionPolicy{
			ID:   srv.ID + "/connectionPolicies/" + policyName,
			Name: policyName,
			Type: "Microsoft.Sql/servers/connectionPolicies",
			Properties: ConnectionPolicyProperties{
				ConnectionType: "Default",
			},
		})
		return
	}
	server.WriteJSON(w, http.StatusOK, policy)
}
