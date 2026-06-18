package server

import "net/http"

// Version se setea en build time (ver cmd/azure-emulator) o queda en "dev"
// si se compila sin ldflags.
var Version = "dev"

// RegisterHealth monta un endpoint simple para smoke-testing: confirma que
// el proceso está arriba y qué versión corre, sin tocar la base de datos.
func RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": Version,
		})
	})
}
