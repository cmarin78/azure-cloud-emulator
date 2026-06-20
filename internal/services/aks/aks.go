// Package aks emula el subconjunto de Microsoft.ContainerService necesario
// para que az CLI/Terraform (azurerm_kubernetes_cluster +
// azurerm_kubernetes_cluster_node_pool) puedan crear/leer/borrar un cluster
// AKS de punta a punta. No hay ningún control plane de Kubernetes real
// detrás de este emulador — managedClusters y agentPools son únicamente
// registros "shape-compatible" que siempre reportan provisioningState
// "Succeeded" y powerState "Running", siguiendo el mismo enfoque
// "shape-compatible, no behavior-complete" usado por el resto de los
// servicios de este proyecto (p.ej. Key Vault con material criptográfico
// simulado, o Monitor con metricAlerts que nunca se evalúan).
//
// Al igual que las virtual machines de Compute, tanto managedClusters como
// agentPools son asíncronos (LRO vía internal/server.Operations): en Azure
// real crear/borrar un cluster o un node pool es la operación de larga
// duración por excelencia, y az CLI/Terraform hacen polling real sobre
// Azure-AsyncOperation/Location.
package aks

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.ContainerService (managedClusters, agentPools).
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de AKS.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Register monta todas las rutas de Microsoft.ContainerService en mux.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerManagedClusters(mux)
	s.registerAgentPools(mux)
}
