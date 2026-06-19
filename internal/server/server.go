// Package server arma el http.Server principal del emulador, montando las
// rutas de cada servicio bajo el mismo shape que usa Azure Resource Manager
// (/subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}),
// de forma que az CLI, el provider de Terraform (azurerm) y los SDKs
// oficiales puedan apuntar al emulador sobreescribiendo su endpoint base
// sin parches adicionales.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Server agrupa el mux principal y permite registrar routers de servicios.
type Server struct {
	mux *http.ServeMux
}

func New() *Server {
	return &Server{mux: http.NewServeMux()}
}

// Mux expone el ServeMux subyacente para que cada servicio registre sus rutas.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Handler devuelve el http.Handler final, con logging, recuperación de
// panics y CORS habilitado para que la consola web pueda llamar al
// emulador desde otro puerto/origen.
func (s *Server) Handler() http.Handler {
	return withCORS(withLogging(withRecover(withARMCaseNormalization(s.mux))))
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic atendiendo %s %s: %v", r.Method, r.URL.Path, rec)
				WriteError(w, http.StatusInternalServerError, "InternalServerError",
					"el emulador encontró un error interno procesando la solicitud")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WriteJSON serializa v como JSON con el status code dado. Helper común
// para todos los handlers de servicios.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error escribiendo respuesta JSON: %v", err)
	}
}

// APIError replica el formato de error estándar de ARM:
// {"error": {"code": "ResourceNotFound", "message": "..."}}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]APIError{"error": {Code: code, Message: message}})
}
