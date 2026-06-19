// Package monitor emula un subconjunto de Azure Monitor y Log Analytics
// suficiente para que `az`/Terraform (`azurerm_log_analytics_workspace`,
// `azurerm_monitor_action_group`, `azurerm_monitor_metric_alert`) operen
// extremo a extremo: workspaces de Log Analytics (Microsoft.OperationalInsights),
// grupos de acción y reglas de alerta de métricas (Microsoft.Insights), todos
// ARM CRUD síncrono -- mismo nivel de esfuerzo que VNets/NICs/disks/vaults en
// fases anteriores, ya que ninguno de los tres requiere polling en los flujos
// comunes de az CLI/Terraform.
//
// No hay pipeline de métricas/logs real: las alertas nunca se evalúan, los
// receptores de notificación se persisten tal cual sin enviar nada, y el
// stub de Log Analytics Query API (ver dataplane.go) siempre devuelve un
// resultado vacío -- mismo enfoque de "stub" que gcp-emulator usa para su
// propio paquete monitoring/logging (ver D:\Projects\claude\gcp-emulator\internal\services\monitoring).
//
// Fuera de alcance por ahora (queda para una fase futura si hace falta):
// Microsoft.Insights/diagnosticSettings, porque su ARM ID se anida bajo la
// URI de *cualquier* recurso existente (`{resourceUri}/providers/
// microsoft.insights/diagnosticSettings/{name}`), lo cual requeriría un
// patrón de ruta de profundidad variable -- a diferencia de los tres
// recursos de este paquete, que cuelgan directamente de un resource group
// como cualquier otro recurso ARM normal.
package monitor

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de Monitor/Log
// Analytics (workspaces, action groups, metric alerts, y el stub de query).
type Service struct {
	db *storage.DB
}

// New crea el servicio de Monitor/Log Analytics.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta tanto las rutas ARM (control plane) como el stub de data
// plane de Log Analytics Query API. A diferencia de blob/queue/table/keyvault/
// servicebus/cosmosdb, ninguna ruta de este paquete usa el shape
// "{account}.{servicio}/..." -- todas son rutas ARM normales (bajo
// /subscriptions/...) o una ruta literal de un solo nivel
// (/v1/workspaces/{id}/query, mismo patrón que armmeta/aadtoken/graph), así
// que no hace falta tocar el dispatcher compartido registerDataPlane en
// cmd/azure-emulator/main.go.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerWorkspaces(mux)
	s.registerActionGroups(mux)
	s.registerMetricAlerts(mux)
	s.registerDataPlane(mux)
}
