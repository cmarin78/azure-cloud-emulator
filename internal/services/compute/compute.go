// Package compute emula el subconjunto de Microsoft.Compute necesario para
// que `az`/Terraform puedan crear una VM de punta a punta: managed disks,
// un catálogo estático de imágenes, y virtual machines (con start/stop).
//
// A diferencia de internal/services/network (síncrono), las mutaciones de
// virtual machines sí pasan por el helper de LRO de internal/server,
// porque en Azure real crear/borrar/arrancar/detener una VM es la
// operación asíncrona por excelencia y az CLI/Terraform hacen polling real
// sobre Azure-AsyncOperation — vale la pena emular esa latencia/forma de
// respuesta aquí. Managed disks, en cambio, se quedan síncronos (igual que
// las VNets/NICs) porque su "Effort" en el roadmap es S, no L.
package compute

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/network"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Compute (disks, images, virtual machines).
type Service struct {
	db  *storage.DB
	ops *server.Operations
	net *network.Service
}

// New crea el servicio de Compute. net se usa para validar que las VMs
// referencien NICs existentes (properties.networkProfile.networkInterfaces).
func New(db *storage.DB, ops *server.Operations, net *network.Service) *Service {
	return &Service{db: db, ops: ops, net: net}
}

// Register monta todas las rutas de Microsoft.Compute en mux.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerDisks(mux)
	s.registerImages(mux)
	s.registerVirtualMachines(mux)
}
