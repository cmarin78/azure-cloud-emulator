// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/devtls"
	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/services/aadtoken"
	"github.com/cesarmarin/azure-emulator/internal/services/aks"
	"github.com/cesarmarin/azure-emulator/internal/services/appservice"
	"github.com/cesarmarin/azure-emulator/internal/services/armmeta"
	"github.com/cesarmarin/azure-emulator/internal/services/authorization"
	"github.com/cesarmarin/azure-emulator/internal/services/blob"
	"github.com/cesarmarin/azure-emulator/internal/services/compute"
	"github.com/cesarmarin/azure-emulator/internal/services/cosmosdb"
	"github.com/cesarmarin/azure-emulator/internal/services/functions"
	"github.com/cesarmarin/azure-emulator/internal/services/graph"
	"github.com/cesarmarin/azure-emulator/internal/services/keyvault"
	"github.com/cesarmarin/azure-emulator/internal/services/managedidentity"
	"github.com/cesarmarin/azure-emulator/internal/services/monitor"
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
	publicURL := flag.String("public-url", envOr("AZURE_EMULATOR_PUBLIC_URL", ""), "URL pública con la que los clientes alcanzan a este emulador (para el documento de metadata de ARM); por defecto se deriva de -addr y -tls asumiendo localhost")
	enableTLS := flag.Bool("tls", envOr("AZURE_EMULATOR_TLS", "") != "", "servir HTTPS con un certificado autofirmado (generado si no existe). Requerido para que az CLI / azurerm completen login real, ya que ambos rechazan un cloud personalizado en HTTP plano")
	tlsCert := flag.String("tls-cert", envOr("AZURE_EMULATOR_TLS_CERT", ""), "ruta al certificado TLS (PEM); si está vacío se deriva de -db y se autogenera de no existir")
	tlsKey := flag.String("tls-key", envOr("AZURE_EMULATOR_TLS_KEY", ""), "ruta a la llave privada TLS (PEM); si está vacío se deriva de -db y se autogenera de no existir")
	flag.Parse()

	scheme := "http://"
	if *enableTLS {
		scheme = "https://"
	}

	base := *publicURL
	if base == "" {
		host := *addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		base = scheme + host
	}
	base = strings.TrimSuffix(base, "/")

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
	monitor.New(db).Register(srv.Mux())
	appservice.New(db).Register(srv.Mux())
	functions.New(db).Register(srv.Mux())
	aks.New(db, ops).Register(srv.Mux())
	authorization.New(db).Register(srv.Mux())
	managedidentity.New(db).Register(srv.Mux())
	registerDataPlane(srv.Mux(), db, keyVaultSvc, serviceBusSvc, cosmosSvc)

	// Descubrimiento de metadata ARM + emisor de tokens AAD falso: permiten
	// que `az cloud register`/`az login --service-principal` y el provider
	// de Terraform `azurerm` (con `environment = "custom"`) apunten a este
	// emulador en vez de depender de `az rest`/el provider `http` genérico.
	armmeta.New().Register(srv.Mux(), base)
	aadtoken.New().Register(srv.Mux())
	graph.New(db).Register(srv.Mux())

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

	log.Printf("azure-emulator listening on %s (data: %s, web: %s, web enabled: %v, public-url: %s, tls: %v)", *addr, *dbPath, *staticDir, webEnabled, base, *enableTLS)

	if *enableTLS {
		certPath, keyPath := *tlsCert, *tlsKey
		dataDir := filepath.Dir(*dbPath)
		if certPath == "" {
			certPath = filepath.Join(dataDir, "tls", "cert.pem")
		}
		if keyPath == "" {
			keyPath = filepath.Join(dataDir, "tls", "key.pem")
		}
		if err := devtls.EnsureSelfSigned(certPath, keyPath); err != nil {
			log.Fatalf("azure-emulator: %v", err)
		}
		log.Printf("azure-emulator: usando certificado TLS autofirmado en %s — debes confiar en él explícitamente (ver \"Habilitar HTTPS\" en README.md) para que az CLI/azurerm lo acepten", certPath)
		if err := http.ListenAndServeTLS(*addr, certPath, keyPath, srv.Handler()); err != nil {
			log.Fatalf("azure-emulator: %v", err)
		}
		return
	}

	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("azure-emulator: %v", err)
	}
}

// registerDataPlane monta el dispatcher compartido para los servicios de
// data plane "path-style" (blob, queue, table, Service Bus y Cosmos DB).
// Todos sirven bajo el shape "/{account}.{servicio}/{resto-del-path}", que
// en net/http.ServeMux es exactamente el mismo patrón de wildcards
// ("/{x}/{y...}") sin importar el nombre que cada paquete le dé a su
// wildcard — registrar uno por servicio (como hace cada ARM control-plane
// service con su propio Register) provoca un panic en tiempo de arranque
// ("conflicts with pattern"). Por eso este es el único lugar que llama
// mux.HandleFunc para estos servicios: lee el primer segmento del path
// una vez y despacha por sufijo al ServeHTTP del servicio que corresponda.
func registerDataPlane(mux *http.ServeMux, db *storage.DB, keyVaultSvc *keyvault.Service, serviceBusSvc *servicebus.Service, cosmosSvc *cosmosdb.Service) {
	blobSvc := blob.New(db)
	queueSvc := queue.New(db)
	tableSvc := table.New(db)

	mux.HandleFunc("/{accountResource}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		accountResource := r.PathValue("accountResource")
		switch {
		case strings.HasSuffix(accountResource, ".blob"):
			blobSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".queue"):
			queueSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".table"):
			tableSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".vault"):
			keyVaultSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".servicebus"):
			serviceBusSvc.ServeHTTP(w, r)
		case strings.HasSuffix(accountResource, ".documents"):
			cosmosSvc.ServeHTTP(w, r)
		default:
			server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
				"endpoint de data plane desconocido: se esperaba el shape '{account}.blob/...', '{account}.queue/...', '{account}.table/...', '{vault}.vault/...', '{namespace}.servicebus/...' o '{account}.documents/...'")
		}
	})
}
