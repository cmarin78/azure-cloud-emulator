// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

func main() {
	addr := flag.String("addr", ":10000", "address to listen on")
	dbPath := flag.String("db", ".azure-emulator-data/azure-emulator.db", "path to the embedded BoltDB data file")
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

	// TODO: register service packages here as they're implemented
	// (Resource Manager, Storage, Compute, Key Vault, ...), following
	// the internal/services/<name>.Register(mux, db, ops) pattern.

	log.Printf("azure-emulator listening on %s (data: %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
}
