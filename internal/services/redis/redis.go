// Package redis emula el subconjunto de Microsoft.Cache necesario para que
// az/Terraform (azurerm_redis_cache) operen extremo a extremo:
// instancias de Redis Cache (ARM CRUD síncrono) con propiedades fake
// deterministas -- hostName, port, sslPort, primaryKey, secondaryKey,
// primaryConnectionString -- y la acción POST listKeys.
//
// No hay motor de cache real: las instancias son registros ARM con
// propiedades fake, "shape-compatible, no behavior-complete" igual que
// el resto del proyecto.
package redis

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.Cache (redis).
type Service struct {
	db *storage.DB
}

// New crea el servicio de Redis Cache.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta las rutas ARM de Microsoft.Cache.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerRedis(mux)
}
