// Package containerinstances emula el subconjunto de
// Microsoft.ContainerInstance necesario para az CLI y Terraform
// (azurerm_container_group).
package containerinstances

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado de la emulación de Container Instances.
type Service struct{ db *storage.DB }

// New construye un nuevo Service.
func New(db *storage.DB) *Service { return &Service{db: db} }

// Register registra todas las rutas ARM de Microsoft.ContainerInstance.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerGroups(mux)
}
