// Package appconfiguration implements the Microsoft.AppConfiguration ARM control-plane
// endpoints and simulated data-plane for Azure App Configuration stores and key-values.
package appconfiguration

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service provides ARM control-plane and data-plane operations for Azure App Configuration.
type Service struct {
	db *storage.DB
}

// New creates a new App Configuration service.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register mounts the ARM control-plane routes for Microsoft.AppConfiguration.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerStores(mux)
	s.registerKeyValues(mux)
}
