// Package eventhub emula el subconjunto de Microsoft.EventHub necesario
// para que az/Terraform (azurerm_eventhub_namespace, azurerm_eventhub,
// azurerm_eventhub_consumer_group) operen extremo a extremo: namespaces
// (ARM CRUD, asíncrono -- igual que servicebus.Namespace, aprovisiona
// infraestructura real en Azure), event hubs y consumer groups (ARM CRUD
// síncrono, sub-recursos anidados de un solo nivel, igual que
// servicebus.Topic/Subscription), y un data-plane de envío/recepción
// simplificado.
//
// El data-plane real de Event Hubs (AMQP/Kafka, particiones, checkpoints
// vía Blob Storage) está fuera de alcance: aquí send/receive es una sola
// cola FIFO por event hub con offsets numéricos crecientes, sin
// particiones reales ni coordinación de checkpoint entre consumidores --
// "shape-compatible, no behavior-complete" (ver dataplane.go para el
// detalle del modelo simplificado).
//
// Nota de naming: en Azure real, Event Hubs también expone su data-plane
// bajo "{namespace}.servicebus.windows.net" (el mismo dominio que Service
// Bus, distinguido por el SDK/protocolo, no por el hostname). Esta
// convención path-style del emulador, en cambio, despacha por sufijo del
// primer segmento del path (ver cmd/azure-emulator/main.go), así que
// reutilizar ".servicebus" colisionaría con el dispatcher de Service Bus;
// se usa ".eventhub" como sufijo distintivo -- una divergencia deliberada
// y documentada del shape real, igual que la mayoría de las
// simplificaciones de data-plane de este proyecto.
package eventhub

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.EventHub (namespaces, eventhubs, consumergroups) y el
// data-plane simplificado de envío/recepción.
type Service struct {
	db  *storage.DB
	ops *server.Operations
}

// New crea el servicio de Event Hubs.
func New(db *storage.DB, ops *server.Operations) *Service {
	return &Service{db: db, ops: ops}
}

// Register monta las rutas ARM (control plane) de Microsoft.EventHub. El
// data-plane de envío/recepción ("/{namespace}.eventhub/...") no se
// registra aquí: sigue la misma convención path-style que blob/queue/.../
// servicebus, así que se monta a través del dispatcher compartido en
// cmd/azure-emulator/main.go (ver ServeHTTP en dataplane.go).
func (s *Service) Register(mux *http.ServeMux) {
	s.registerNamespaces(mux)
	s.registerHubs(mux)
}
