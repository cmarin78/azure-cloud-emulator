// Package graph emula el único endpoint de Microsoft Graph que el
// provider de Terraform `azurerm` necesita cuando el access token emitido
// no incluye el claim "oid" (que es justo el caso de nuestro emisor de
// tokens falso en internal/services/aadtoken, ya que no simula ningún
// directorio real): GET /v1.0/servicePrincipals?$filter=appId eq
// '{clientId}', usado para descubrir el object ID del service principal
// autenticado.
//
// Como el documento de metadata de ARM (ver internal/services/armmeta)
// anuncia tanto "graph" como "microsoftGraphResourceId" apuntando de
// vuelta al propio emulador (en vez de a graph.microsoft.com), azurerm
// termina llamando a este mismo proceso en vez de a Microsoft real — así
// que no hace falta ningún token ni validación adicional, solo devolver
// una forma de respuesta plausible.
package graph

import (
	"crypto/sha256"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// Service no necesita estado: el object ID que devolvemos es determinista
// (derivado del appId solicitado), así que no hace falta persistirlo.
type Service struct{}

func New() *Service {
	return &Service{}
}

// servicePrincipal replica el subconjunto mínimo de la forma real de
// Microsoft Graph que azurerm lee de la respuesta.
type servicePrincipal struct {
	ID          string `json:"id"`
	AppID       string `json:"appId"`
	DisplayName string `json:"displayName"`
}

// Register monta la única ruta de Graph que emulamos. Se registra como
// prefijo literal "/v1.0/" (no wildcard) para que sea estrictamente más
// específico que el dispatcher de data-plane compartido
// ("/{accountResource}/{path...}" en cmd/azure-emulator/main.go) — el
// mismo conflicto de net/http.ServeMux que ya se resolvió para
// internal/services/aadtoken con el prefijo "/login/".
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1.0/servicePrincipals", s.listServicePrincipals)
}

func (s *Service) listServicePrincipals(w http.ResponseWriter, r *http.Request) {
	appID := extractAppIDFilter(r.URL.Query().Get("$filter"))
	if appID == "" {
		server.WriteJSON(w, http.StatusOK, map[string]any{"value": []servicePrincipal{}})
		return
	}

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"value": []servicePrincipal{
			{
				ID:          fakeObjectID(appID),
				AppID:       appID,
				DisplayName: "azure-emulator fake service principal",
			},
		},
	})
}

// extractAppIDFilter interpreta el único shape de "$filter" que azurerm
// envía: appId eq '{valor}' (con comillas simples). No es un parser
// general de OData — basta con lo que el cliente real manda.
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

// fakeObjectID deriva un GUID estable a partir del appId, para que el
// mismo service principal "falso" siempre tenga el mismo object ID entre
// reinicios del emulador (sin necesidad de persistirlo en BoltDB).
func fakeObjectID(appID string) string {
	sum := sha256.Sum256([]byte("azure-emulator-fake-object-id:" + appID))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
