// Package apimanagement emula el subconjunto de Microsoft.ApiManagement
// necesario para que az CLI/Terraform (azurerm_api_management +
// azurerm_api_management_api/_api_operation/_product/_product_api/
// _subscription) puedan crear/leer/borrar una instancia de API Management
// de punta a punta. No hay ningún gateway real detrás de este emulador:
// no se hace proxying de requests, no se evalúan policies, no hay
// developer portal — service/apis/operations/products/subscriptions son
// únicamente registros "shape-compatible" que siempre reportan
// provisioningState "Succeeded", siguiendo el mismo enfoque
// "shape-compatible, no behavior-complete" usado por el resto de los
// servicios de este proyecto (p.ej. AKS con managedClusters, o Key Vault
// con material criptográfico simulado).
//
// La instancia de servicio (Microsoft.ApiManagement/service) es asíncrona
// (LRO vía internal/server.Operations): en Azure real, aprovisionar una
// instancia de APIM tarda entre 30 y 45 minutos, el candidato perfecto
// para simular como una LRO larga que siempre termina en éxito. Los
// sub-recursos (apis, operations, products, subscriptions, y la
// asociación product-api) son síncronos, igual que las reglas de
// seguridad de un NSG o los topics/event subscriptions de Event Grid.
package apimanagement

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.ApiManagement.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de API Management.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Register monta todas las rutas de Microsoft.ApiManagement en mux.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerServices(mux)
	s.registerAPIs(mux)
	s.registerProducts(mux)
}
