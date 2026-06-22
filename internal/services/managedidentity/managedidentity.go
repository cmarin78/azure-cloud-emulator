// Package managedidentity emula Microsoft.ManagedIdentity/userAssignedIdentities
// (Phase 16): el recurso standalone que azurerm_user_assigned_identity crea y
// que luego otros recursos referencian por ID dentro de su propio bloque
// "identity" (tipo "UserAssigned"/"SystemAssigned, UserAssigned").
//
// Es ARM CRUD síncrono, mismo Effort "S" que App Service Plans/vaults/disks:
// crear una identidad no requiere polling en los flujos comunes de az
// CLI/Terraform. No hay runtime real detrás -- principalId/clientId/tenantId
// son valores deterministas derivados del ID del recurso (mismo patrón que
// aks.fakeGUID/compute.fakeGUID/appservice.fakeGUID), así que GETs repetidos
// siempre devuelven los mismos valores sin necesidad de un directorio Entra
// ID real detrás.
//
// El sub-objeto "identity" (SystemAssigned) de otros recursos (App Service
// sites, Compute VMs, AKS clusters) vive en sus propios paquetes -- ver
// compute.Identity/appservice.Identity/aks.Identity -- y no depende de este
// paquete: Azure real tampoco modela un userAssignedIdentities real para el
// caso SystemAssigned, solo principalId/tenantId sintéticos por recurso.
//
// Fuera de alcance por ahora: asignar una identidad a un recurso vía
// referencia por ID en el bloque "identity.userAssignedIdentities" (el mapa
// {resourceId: {}} que azurerm_linux_web_app/azurerm_kubernetes_cluster
// pueblan) -- no se valida ni resuelve esa referencia, solo se persiste tal
// cual el cliente la envía, igual que metricAlerts.actionGroupId u otras
// referencias cruzadas "sin integridad referencial estricta" en este
// proyecto.
package managedidentity

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.ManagedIdentity.
type Service struct {
	db *storage.DB
}

// New crea el servicio de Managed Identity.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta todas las rutas de Microsoft.ManagedIdentity en mux.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerUserAssignedIdentities(mux)
}
