// Package authorization emula el subconjunto de Microsoft.Authorization
// (Azure RBAC) necesario para azurerm_role_definition/azurerm_role_assignment
// y `az role definition`/`az role assignment`: definiciones de rol
// personalizadas y asignaciones de rol, ambos ARM CRUD síncrono -- mismo
// nivel de esfuerzo que NSGs/Route Tables en Fase 12, ya que ninguno de los
// dos requiere polling en los flujos comunes de az CLI/Terraform.
//
// Sin integridad referencial, mismo enfoque que el resto del proyecto
// (compute no valida el NIC referenciado, functions no valida el site
// padre): roleAssignments no valida que roleDefinitionId ni principalId
// existan realmente -- ambos se persisten y devuelven tal cual.
//
// roleDefinitions solo se emula a nivel de suscripción (el scope que usan
// azurerm_role_definition/`az role definition create` por defecto).
// roleAssignments se emula a nivel de suscripción y de resource group (los
// dos scopes que cubren la inmensa mayoría de los casos reales); el scope a
// nivel de un recurso individual ("/subscriptions/.../providers/Microsoft.X/
// .../roleAssignments/{id}") queda fuera de alcance por la misma razón que
// Microsoft.Insights/diagnosticSettings en internal/services/monitor: su
// ARM ID se ancla bajo la URI de *cualquier* recurso existente, lo que
// requeriría un patrón de ruta de profundidad variable.
package authorization

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Authorization.
type Service struct {
	db *storage.DB
}

// New crea el servicio de Authorization (RBAC).
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta todas las rutas de este paquete en mux. Son rutas ARM
// normales bajo /subscriptions/... -- no toca el dispatcher compartido
// registerDataPlane.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerRoleDefinitions(mux)
	s.registerRoleAssignments(mux)
}
