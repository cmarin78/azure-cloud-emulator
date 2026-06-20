// Package functions emula el sub-recurso Microsoft.Web/sites/functions de
// Azure Functions: las funciones individuales desplegadas dentro de un
// "function app". Un function app en Azure real ES un
// Microsoft.Web/sites (el mismo recurso que un Web App, distinguido por
// kind="functionapp"/"functionapp,linux"), así que el ARM CRUD del site en
// sí ya lo cubre internal/services/appservice sin cambios — putSite acepta
// cualquier valor de "kind" tal cual. Lo que falta, y lo que añade este
// paquete, es lo que es exclusivo de Functions: el listado/CRUD de
// funciones individuales (az functionapp function list/show/delete) y la
// acción síncrona syncfunctiontriggers/host listKeys que az CLI/tooling de
// despliegue invocan tras publicar código.
//
// Mismo enfoque "shape-compatible, no behavior-complete" que el resto del
// proyecto: no hay runtime real, ningún trigger se ejecuta de verdad, y
// "config" se persiste tal cual sin validarse contra el shape real de
// function.json. No se valida que el site padre exista — mismo
// no-validación de referencias cruzadas usado en monitor (actionGroupId) y
// appservice (serverFarmId).
package functions

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Web/sites/functions y las acciones de sync relacionadas.
type Service struct {
	db *storage.DB
}

// New crea el servicio de Azure Functions.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta todas las rutas bajo mux. Igual que appservice/monitor,
// son rutas ARM normales bajo /subscriptions/... — no toca el dispatcher
// compartido registerDataPlane.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerFunctions(mux)
}
