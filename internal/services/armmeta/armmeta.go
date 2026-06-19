// Package armmeta emula el endpoint de descubrimiento de metadata de Azure
// Resource Manager: GET /metadata/endpoints. Este es el documento que
// `az cloud register --endpoint-resource-manager <url>` (y, por extensión,
// el provider de Terraform `azurerm` cuando se le pasa `metadata_host` con
// `environment = "custom"`) consultan para descubrir el resto de los
// endpoints del "cloud" (login, graph, sufijos de DNS por servicio, etc.)
// a partir de una sola URL base.
//
// No hay autenticación ni validación real: el documento es estático y
// siempre apunta de vuelta al propio emulador para `resourceManager` y al
// emisor de tokens falso (ver internal/services/aadtoken) para el endpoint
// de login.
package armmeta

import (
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// Service no necesita estado: el documento de metadata es el mismo sin
// importar qué base URL use el cliente (Azure real tampoco varía este
// documento por tenant/suscripción).
type Service struct{}

func New() *Service {
	return &Service{}
}

// authentication replica "authentication" dentro del documento de metadata.
type authentication struct {
	LoginEndpoint    string   `json:"loginEndpoint"`
	Audiences        []string `json:"audiences"`
	Tenant           string   `json:"tenant"`
	IdentityProvider string   `json:"identityProvider"`
}

// suffixes replica el subconjunto de "suffixes" que los SDKs/Terraform
// realmente leen para construir hostnames de data-plane (storage, vault,
// etc.) cuando el cloud es "custom".
type suffixes struct {
	Storage                          string `json:"storage"`
	StorageSyncEndpointSuffix        string `json:"storageSyncEndpointSuffix,omitempty"`
	KeyVaultDNS                      string `json:"keyVaultDns"`
	MHSMDNS                          string `json:"mhsmDns,omitempty"`
	SQLServerHostname                string `json:"sqlServerHostname"`
	AzureDataLakeStoreFileSystem     string `json:"azureDataLakeStoreFileSystem,omitempty"`
	AzureDataLakeAnalyticsCatalogJob string `json:"azureDataLakeAnalyticsCatalogAndJob,omitempty"`
	ACRLoginServer                   string `json:"acrLoginServer,omitempty"`
}

// cloudMetadata replica el shape que `az cloud register` (internamente
// azure.cli.core.cloud._arm_to_cli_mapper) espera de
// GET /metadata/endpoints?api-version=2022-09-01. Los nombres de campo
// importan exactamente: la versión instalada de az CLI fallaba con
// "KeyError: 'name'" porque el documento no incluía ese campo, y luego con
// claves equivocadas ("galleryEndpoint" en vez de "gallery",
// "portalEndpoint" en vez de "portal") — confirmado leyendo las constantes
// de string embebidas en azure/cli/core/cloud.pyc instalado localmente, ya
// que esa lógica no está documentada públicamente con el mismo detalle que
// el resto del protocolo de descubrimiento.
type cloudMetadata struct {
	Name            string         `json:"name"`
	Portal          string         `json:"portal"`
	Gallery         string         `json:"gallery"`
	Graph           string         `json:"graph"`
	Authentication  authentication `json:"authentication"`
	ResourceManager string         `json:"resourceManager"`
	Suffixes        suffixes       `json:"suffixes"`
	Batch           string         `json:"batch"`
	Media           string         `json:"media"`
	GraphAudience   string         `json:"graphAudience"`
	SQLManagement   string         `json:"sqlManagement"`
	// microsoftGraphResourceId se lee con .get(...) en az CLI (tiene
	// default), pero igual lo anunciamos explícitamente para que el SDK de
	// Microsoft Graph (si algún día se usa) también lo descubra.
	MicrosoftGraphResourceID string `json:"microsoftGraphResourceId,omitempty"`
}

// Register monta la ruta de metadata en mux. base es la URL pública con la
// que los clientes alcanzan a este emulador (ej. "http://localhost:10000"),
// usada tanto para "resourceManager" como para "authentication.loginEndpoint"
// (el emisor de tokens falso vive en el mismo proceso/puerto).
func (s *Service) Register(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET /metadata/endpoints", func(w http.ResponseWriter, r *http.Request) {
		doc := cloudMetadata{
			Name:    "AzureEmulator",
			Portal:  base + "/",
			Gallery: base + "/",
			Graph:   base + "/",
			Authentication: authentication{
				// "/login/" en vez de "/" a secas: az CLI/azurerm construyen
				// la URL de token concatenando loginEndpoint + tenantId +
				// "/oauth2/v2.0/token", y ese path debe coincidir con la
				// ruta literal que registra internal/services/aadtoken
				// (ver el comentario en aadtoken.Register sobre por qué no
				// puede vivir en la raíz).
				LoginEndpoint:    base + "/login/",
				Audiences:        []string{base + "/", "https://management.core.windows.net/"},
				Tenant:           "common",
				IdentityProvider: "AAD",
			},
			ResourceManager: base + "/",
			Suffixes: suffixes{
				Storage:           "core.windows.net",
				KeyVaultDNS:       "vault.azure.net",
				SQLServerHostname: "database.windows.net",
			},
			Batch:                    base + "/",
			Media:                    base + "/",
			GraphAudience:            base + "/",
			SQLManagement:            base + "/",
			MicrosoftGraphResourceID: base + "/",
		}
		server.WriteJSON(w, http.StatusOK, doc)
	})
}
