// Package appservice emula un subconjunto de Azure App Service suficiente
// para que `az`/Terraform (`azurerm_service_plan`,
// `azurerm_linux_web_app`/`azurerm_windows_web_app`) operen extremo a
// extremo: App Service Plans (Microsoft.Web/serverfarms) y Web Apps
// (Microsoft.Web/sites), ambos ARM CRUD síncrono -- mismo nivel de
// esfuerzo que VNets/NICs/disks/vaults/Monitor en fases anteriores, ya que
// ninguno de los dos requiere polling en los flujos comunes de az
// CLI/Terraform.
//
// No hay runtime real detrás: un site nunca ejecuta código, "start"/"stop"/
// "restart" solo cambian el campo properties.state, y el StringDictionary
// de app settings (Microsoft.Web/sites/config/appsettings) se persiste tal
// cual sin que ningún proceso lo lea -- mismo enfoque de "stub" que
// gcp-emulator usa para su propio paquete de App Engine/Cloud Run (ver
// D:\Projects\claude\gcp-emulator\internal\services).
//
// Fuera de alcance por ahora (queda para una fase futura si hace falta):
// deployment slots, custom domains/certificate bindings, y
// Microsoft.Web/sites/functions (eso es Azure Functions, un paquete
// separado con su propio Effort).
package appservice

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.Web (App Service Plans + Web Apps + config/appsettings).
type Service struct {
	db *storage.DB
}

// New crea el servicio de App Service.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta todas las rutas de Microsoft.Web en mux. Igual que
// monitor, todas son rutas ARM normales bajo /subscriptions/... -- no hace
// falta tocar el dispatcher compartido registerDataPlane en
// cmd/azure-emulator/main.go.
func (s *Service) Register(mux *http.ServeMux) {
	s.registerPlans(mux)
	s.registerSites(mux)
}
