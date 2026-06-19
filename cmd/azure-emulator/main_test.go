package main

// Este test existe específicamente por la misma clase de bug documentada en
// servicebus/dataplane_index.go (colisión de claves) pero a nivel de
// routing: dos servicios registrando el mismo patrón de http.ServeMux solo
// entra en panic en tiempo de registro cuando Register() de TODOS los
// servicios se llama junto, en el mismo proceso, contra el mismo mux —
// exactamente lo que hace main() y exactamente lo que ningún test unitario
// por paquete ejercita. `go build`/`go vet` no detectan esta clase de bug;
// solo correr el código de registro real lo atrapa. Este test reproduce el
// wiring de main() (mismos servicios, mismo orden) sin levantar un listener
// HTTP real, para que CI atrape un panic de ruta duplicada antes de que
// llegue a una máquina real.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/aadtoken"
	"github.com/cesarmarin/azure-emulator/internal/services/armmeta"
	"github.com/cesarmarin/azure-emulator/internal/services/compute"
	"github.com/cesarmarin/azure-emulator/internal/services/cosmosdb"
	"github.com/cesarmarin/azure-emulator/internal/services/graph"
	"github.com/cesarmarin/azure-emulator/internal/services/keyvault"
	"github.com/cesarmarin/azure-emulator/internal/services/monitor"
	"github.com/cesarmarin/azure-emulator/internal/services/network"
	"github.com/cesarmarin/azure-emulator/internal/services/resourcemanager"
	"github.com/cesarmarin/azure-emulator/internal/services/servicebus"
	"github.com/cesarmarin/azure-emulator/internal/services/storageaccounts"
	"github.com/cesarmarin/azure-emulator/internal/testutil"
)

func TestAllServicesRegisterWithoutPanic(t *testing.T) {
	db := testutil.NewDB(t)
	ops := server.NewOperations()

	srv := server.New()
	mux := srv.Mux()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registering all services together panicked (likely a duplicate "+
				"http.ServeMux route pattern across two services): %v", r)
		}
	}()

	server.RegisterHealth(mux)
	server.RegisterOperations(mux, ops)

	resourcemanager.New(db, ops).Register(mux)
	storageaccounts.New(db, ops).Register(mux)
	networkSvc := network.New(db)
	networkSvc.Register(mux)
	compute.New(db, ops, networkSvc).Register(mux)
	keyVaultSvc := keyvault.New(db)
	keyVaultSvc.Register(mux)
	serviceBusSvc := servicebus.New(db, ops)
	serviceBusSvc.Register(mux)
	cosmosSvc := cosmosdb.New(db, ops)
	cosmosSvc.Register(mux)
	monitor.New(db).Register(mux)
	registerDataPlane(mux, db, keyVaultSvc, serviceBusSvc, cosmosSvc)

	armmeta.New().Register(mux, "http://localhost:10000")
	aadtoken.New().Register(mux)
	graph.New().Register(mux)

	// Sanity check trivial: el mux debería estar lo bastante armado como
	// para devolver *alguna* respuesta (aunque sea un 404) en vez de un
	// nil-dereference.
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /healthz to respond 200, got %d", rec.Code)
	}
}
