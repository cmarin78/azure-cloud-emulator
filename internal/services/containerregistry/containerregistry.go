// Package containerregistry emula el subconjunto de
// Microsoft.ContainerRegistry necesario para que az/Terraform
// (azurerm_container_registry) operen extremo a extremo: registries (ARM
// CRUD síncrono -- crear un registry en Azure real no requiere polling en
// los flujos comunes, igual que managedidentity.UserAssignedIdentity/
// keyvault.Vault).
//
// No hay registro de imágenes real (push/pull docker): loginServer es un
// hostname fake derivado del nombre del registry -- "shape-compatible, no
// behavior-complete" como el resto de los data planes simplificados de
// este proyecto.
package containerregistry

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.ContainerRegistry (registries).
type Service struct {
	db *storage.DB
}

// New crea el servicio de Container Registry.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta las rutas ARM de Microsoft.ContainerRegistry.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerRegistries(mux)
}
