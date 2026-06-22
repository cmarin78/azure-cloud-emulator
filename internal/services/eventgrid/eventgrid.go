// Package eventgrid emula el subconjunto de Microsoft.EventGrid necesario
// para que az/Terraform (azurerm_eventgrid_topic,
// azurerm_eventgrid_event_subscription) operen extremo a extremo: topics
// (ARM CRUD, síncrono — a diferencia de Service Bus namespaces, un topic de
// Event Grid no aprovisiona infraestructura de cómputo pesada, así que no
// hace falta LRO) y event subscriptions (también ARM CRUD síncrono, anidadas
// como "extension resource" bajo el topic).
//
// Fase 20/Fase 17: igual que monitor/actiongroups.go con los
// webhookReceivers de Action Groups, las event subscriptions con
// destination.endpointType="WebHook" sí se despachan de verdad — publicar un
// evento en un topic (ver dataplane.go) dispara un POST HTTP real a cada
// endpoint suscrito. A diferencia de Action Groups (despacho síncrono
// disparado por una acción explícita createNotifications), aquí el publish
// es la ruta caliente de un pipeline de eventos, así que el despacho corre
// en una goroutine "fire-and-forget" por subscription — mismo enfoque que
// gcp-emulator usa para el push real de Pub/Sub
// (D:\Projects\claude\gcp-emulator\internal\services\pubsub): sin
// reintentos ni dead-lettering, documentado como limitación conocida.
//
// No hay evaluación de filtros (subjectBeginsWith/EndsWith,
// advancedFilters): cada evento publicado se entrega a *todas* las event
// subscriptions activas del topic, sin importar lo que declaren sus
// filtros — mismo enfoque "shape-compatible, no behavior-complete" que el
// resto del proyecto (p. ej. Monitor nunca evalúa metricAlerts.criteria).
package eventgrid

import (
	"net/http"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas ARM de
// Microsoft.EventGrid (topics, eventSubscriptions) y el data-plane de
// publish.
//
// httpClient lo usa eventsubscriptions.go para el despacho real de webhooks
// al publicar — mismo patrón (*http.Client con timeout corto, sin
// reintentos) que monitor.Service usa para Action Groups.
type Service struct {
	db         *storage.DB
	httpClient *http.Client
}

// New crea el servicio de Event Grid.
func New(db *storage.DB) *Service {
	return &Service{db: db, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// Register monta las rutas ARM (control plane) de Microsoft.EventGrid. El
// data-plane de publish ("/{topic}.eventgrid/api/events") no se registra
// aquí: como sigue la misma convención path-style que blob/queue/.../
// servicebus, se monta a través del dispatcher compartido en
// cmd/azure-emulator/main.go (ver ServeHTTP en dataplane.go).
func (s *Service) Register(mux *http.ServeMux) {
	s.registerTopics(mux)
	s.registerEventSubscriptions(mux)
}
