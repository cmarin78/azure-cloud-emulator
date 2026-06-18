// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/cesarmarin/azure-emulator/internal/server"
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

	// TODO: register more service packages here as they're implemented
	// (blob/queue/table data plane, Compute, Key Vault, ...), following
	// the internal/services/<name>.New(db, ops).Register(mux) pattern.

	log.Printf("azure-emulator listening on %s (data: %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
}
