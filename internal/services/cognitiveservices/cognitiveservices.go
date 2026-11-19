// Package cognitiveservices implements the Microsoft.CognitiveServices ARM control-plane
// endpoints for Azure Cognitive Services and Azure OpenAI accounts and model deployments.
package cognitiveservices

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service provides ARM control-plane operations for Microsoft.CognitiveServices.
type Service struct {
	db *storage.DB
}

// New creates a new Cognitive Services service instance.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register mounts the ARM control-plane routes for Microsoft.CognitiveServices.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerAccounts(mux)
	s.registerDeployments(mux)
}
