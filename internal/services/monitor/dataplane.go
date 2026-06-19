package monitor

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// registerDataPlane monta el único endpoint de data-plane de este paquete:
// el stub de la Log Analytics Query API. A diferencia de blob/queue/table/
// keyvault/servicebus/cosmosdb, esta no es una API "{cuenta}.{servicio}/..."
// con un identificador de cuenta variable en el primer segmento del path --
// el workspaceId va en el path pero la ruta en sí es un literal de un solo
// nivel (mismo estilo que armmeta/aadtoken/graph), así que se registra
// directamente en mux en vez de pasar por el dispatcher compartido
// registerDataPlane de cmd/azure-emulator/main.go.
//
// No hay datos de logs reales que consultar (no se ingiere nada), así que
// esto siempre responde una tabla vacía -- mismo enfoque de stub que
// gcp-emulator usa para GET /v3/projects/{project}/timeSeries.
func (s *Service) registerDataPlane(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/workspaces/{workspaceId}/query", s.queryWorkspace)
}

func (s *Service) queryWorkspace(w http.ResponseWriter, r *http.Request) {
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"tables": []any{
			map[string]any{
				"name":    "PrimaryResult",
				"columns": []any{},
				"rows":    []any{},
			},
		},
	})
}
