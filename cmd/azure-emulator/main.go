// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/blob"
	"github.com/cesarmarin/azure-emulator/internal/services/queue"
	"github.com/cesarmarin/azure-emulator/internal/services/resourcemanager"
	"github.com/cesarmarin/azure-emulator/internal/services/storageaccounts"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// envOr devuelve la variable de entorno key si está definida, o def en
// caso contrario. Permite que docker-compose / docker run sobreescriban
// los defaults sin tener que pasar flags.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", envOr("AZURE_EMULATOR_ADDR", ":10000"), "address to listen on")
	dbPath := flag.String("db", envOr("AZURE_EMULATOR_DB", ".azure-emulator-data/azure-emulator.db"), "path to the embedded BoltDB data file")
	flag.Parse()

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
	defer db.Close()

	srv := server.New()
	ops := server.NewOperations()
	server.RegisterHealth(srv.Mux())
	server.RegisterOperations(srv.Mux(), ops)

	resourcemanager.New(db, ops).Register(srv.Mux())
	storageaccounts.New(db, ops).Register(srv.Mux())
	registerDataPlane(srv.Mux(), db)

	// TODO: register more ARM control-plane service packages here as
	// they're implemented (Compute, Key Vault, ...), following the
	// internal/services/<name>.New(db, [ops]).Register(mux) pattern.

	log.Printf("azure-emulator listening on %s (data: %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
}

// registerDataPlane monta el dispatcher compartido para los servicios de
// data plane "path-style" (blob, queue, y eventualmente table). Todos
// sirven bajo el shape "/{account}.{servicio}/{resto-del-path}", que en
// net/http.ServeMux es exactamente el mismo patrón de wildcards
// ("/{x}/{y...}") sin importar el nombre que cada paquete le dé a su
// wildcard — registrar uno por servicio (como hace cada ARM control-plane
// service con su propio Register) provoca un panic en tiempo de arranque
// ("conflicts with pattern"). Por eso este es el único lugar que llama
// mux.HandleFunc para estos servicios: lee el primer segmento del path
// una vez y despacha por sufijo al ServeHTTP del servicio que corresponda.
func registerDataPlane(mux *http.ServeMux, db *storage.DB) {
	blobSvc := blob.New(db)
	queueSvc := queue.New(db)

	mux.HandleFunc("/{accountResource}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		accountResource := r.PathValue("accountResource")
		switch {
		case strings.HasSuffix(accountResource, ".blob"):
			blobSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".queue"):
			queueSvc.ServeHTTP(w, r)
		default:
			server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
				"endpoint de data plane desconocido: se esperaba el shape '{account}.blob/...' o '{account}.queue/...'")
		}
	})
}
