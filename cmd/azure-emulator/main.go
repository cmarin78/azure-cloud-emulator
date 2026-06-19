// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/blob"
	"github.com/cesarmarin/azure-emulator/internal/services/compute"
	"github.com/cesarmarin/azure-emulator/internal/services/cosmosdb"
	"github.com/cesarmarin/azure-emulator/internal/services/keyvault"
	"github.com/cesarmarin/azure-emulator/internal/services/network"
	"github.com/cesarmarin/azure-emulator/internal/services/queue"
	"github.com/cesarmarin/azure-emulator/internal/services/resourcemanager"
	"github.com/cesarmarin/azure-emulator/internal/services/servicebus"
	"github.com/cesarmarin/azure-emulator/internal/services/storageaccounts"
	"github.com/cesarmarin/azure-emulator/internal/services/table"
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
	staticDir := flag.String("web", envOr("AZURE_EMULATOR_WEB", "web/console"), "directorio del frontend (consola web)")
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
	networkSvc := network.New(db)
	networkSvc.Register(srv.Mux())
	compute.New(db, ops, networkSvc).Register(srv.Mux())
	keyVaultSvc := keyvault.New(db)
	keyVaultSvc.Register(srv.Mux())
	serviceBusSvc := servicebus.New(db, ops)
	serviceBusSvc.Register(srv.Mux())
	cosmosSvc := cosmosdb.New(db, ops)
	cosmosSvc.Register(srv.Mux())
	registerDataPlane(srv.Mux(), db, keyVaultSvc, serviceBusSvc, cosmosSvc)

	// La consola web se sirve bajo el prefijo "/console/" en vez de "/" a
	// secas: el dispatcher de data plane registra "/{accountResource}/{path...}",
	// y net/http.ServeMux trata ese patrón como un "subtree" — cualquier
	// request de un solo segmento sin slash final (ej. "/style.css") se
	// redirige primero a "/style.css/" y ese segundo intento sí encaja en
	// el wildcard (accountResource="style.css", path=""), terminando en un
	// 404 "ResourceNotFound" en vez de servir el archivo. Un prefijo
	// literal como "/console/" es más específico que el wildcard, así que
	// ServeMux lo prioriza y nunca llega a generarse ese redirect.
	webEnabled := false
	if info, statErr := os.Stat(*staticDir); statErr == nil && info.IsDir() {
		srv.Mux().Handle("/console/", http.StripPrefix("/console/", http.FileServer(http.Dir(*staticDir))))
		indexPath := filepath.Join(*staticDir, "index.html")
		srv.Mux().HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, indexPath)
		})
		webEnabled = true
	}

	log.Printf("azure-emulator listening on %s (data: %s, web: %s, web enabled: %v)", *addr, *dbPath, *staticDir, webEnabled)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
}

// registerDataPlan