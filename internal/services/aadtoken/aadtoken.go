// Package aadtoken emula el emisor de tokens de Azure AD / Microsoft Entra
// ID lo mínimo necesario para que un flujo de client credentials (el que
// usan `az login --service-principal` y el provider de Terraform `azurerm`
// con client_id/client_secret/tenant_id) complete sin tocar un tenant real.
//
// No hay autenticación real: cualquier client_id/client_secret/scope se
// acepta como válido. La respuesta es la forma mínima que MSAL (la librería
// que usan az CLI, los SDKs de Azure y azurerm por debajo) espera para
// poder cachear el token: un JWT con la forma correcta (header.payload.firma)
// aunque sin firma criptográfica real, más los campos token_type/expires_in
// que el protocolo OAuth2 exige.
package aadtoken

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// Service no necesita estado: cualquier combinación de tenant/client es
// aceptada, así que no hay nada que persistir entre llamadas.
type Service struct{}

func New() *Service {
	return &Service{}
}

// tokenResponse replica el shape mínimo que devuelve el endpoint real de
// token de Azure AD para un flujo de client credentials.
type tokenResponse struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	ExtExpiresIn int    `json:"ext_expires_in"`
	AccessToken  string `json:"access_token"`
}

// Register monta las rutas del emisor de tokens en mux bajo el prefijo
// literal "/login/" en vez de en la raíz: el wildcard de un solo segmento
// que necesita el {tenantId} ("/login/{tenantId}/oauth2/...") conviviría
// mal con cualquier otra ruta de un solo segmento registrada en la raíz
// (como "/console/") — net/http.ServeMux rechaza con panic en arranque dos
// patrones que se solapan sin que ninguno sea estrictamente más específico
// ("/console/oauth2/v2.0/token" encaja en ambos shapes a la vez). Montar
// esto bajo "/login/" lo vuelve más específico que cualquier wildcard de
// primer-segmento y evita el conflicto. El documento de metadata de ARM
// (internal/services/armmeta) anuncia "authentication.loginEndpoint" como
// "<base>/login/", así que az CLI/azurerm construyen exactamente esta URL.
//
// Se registran tanto la v2.0 (usada por el grant flow moderno de MSAL) como
// la v1 (compatibilidad con clientes más viejos basados en ADAL). Ambas
// aceptan cualquier client_id/client_secret/scope y devuelven el mismo
// token falso.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /login/{tenantId}/oauth2/v2.0/token", s.issueToken)
	mux.HandleFunc("POST /login/{tenantId}/oauth2/token", s.issueToken)
}

func (s *Service) issueToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid_request",
			"no se pudo interpretar el cuerpo del formulario (se espera application/x-www-form-urlencoded)")
		return
	}

	tenantID := r.PathValue("tenantId")
	clientID := r.FormValue("client_id")
	// resource (v1) o scope (v2.0): cualquiera de los dos identifica la
	// audiencia para la que se "emite" el token. Si no viene ninguno, se usa
	// la audiencia genérica de ARM.
	audience := r.FormValue("resource")
	if audience == "" {
		audience = r.FormValue("scope")
	}
	if audience == "" {
		audience = "https://management.core.windows.net/"
	}

	const ttl = 3600
	token := fakeJWT(tenantID, clientID, audience, ttl)

	server.WriteJSON(w, http.StatusOK, tokenResponse{
		TokenType:    "Bearer",
		ExpiresIn:    ttl,
		ExtExpiresIn: ttl,
		AccessToken:  token,
	})
}

// fakeJWT construye un JWT sintácticamente válido (header.payload.firma en
// base64url) pero sin firma criptográfica real ("alg": "none"). MSAL parsea
// el payload para extraer claims como exp/tid al cachear el token, así que
// necesita tener esa forma — pero ni az CLI ni azurerm validan la firma
// contra este emulador, solo la adjuntan como Bearer en llamadas siguientes,
// que tampoco se validan server-side.
func fakeJWT(tenantID, clientID, audience string, ttlSeconds int) string {
	now := time.Now().Unix()
	header := map[string]any{
		"alg": "none",
		"typ": "JWT",
	}
	payload := map[string]any{
		"aud":   audience,
		"iss":   "https://login.microsoftonline.com/" + tenantID + "/v2.0",
		"iat":   now,
		"nbf":   now,
		"exp":   now + int64(ttlSeconds),
		"tid":   tenantID,
		"appid": clientID,
		"sub":   clientID,
		"ver":   "1.0",
	}
	return b64Segment(header) + "." + b64Segment(payload) + "." + base64.RawURLEncoding.EncodeToString([]byte("emulated-no-signature"))
}

func b64Segment(v map[string]any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}
