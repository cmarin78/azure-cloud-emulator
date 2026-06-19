package server

import (
	"net/http"
	"strings"
)

// armCanonicalSegments mapea (en minúsculas) -> forma canónica exacta que
// usan los patrones registrados con mux.HandleFunc en cada paquete de
// internal/services. Existe porque Azure trata los segmentos "fijos" de un
// resource ID de ARM (resourceGroups, providers, namespaces de provider,
// nombres de tipo de recurso) como case-insensitive, pero
// net/http.ServeMux compara los segmentos literales de un patrón de forma
// case-sensitive.
//
// Esto se confirmó empíricamente: el provider de Terraform `azurerm` (vía
// el paquete go-azure-sdk/hashicorp/go-azure-helpers, que normaliza los
// resource IDs que construye a minúsculas para los segmentos fijos) envía
// PUT /subscriptions/{id}/resourcegroups/{name} (todo en minúsculas) en vez
// de PUT /subscriptions/{id}/resourceGroups/{name} — sin este normalizador
// esa solicitud cae en el dispatcher de data-plane compartido en vez de en
// internal/services/resourcemanager, y responde 404 ResourceNotFound.
//
// Los segmentos "variables" de un resource ID (subscriptionId,
// resourceGroupName, nombres de recursos) nunca aparecen en este mapa, así
// que nunca se tocan — solo se normalizan los tokens fijos conocidos que
// usan los patrones de ruta.
var armCanonicalSegments = buildArmCanonicalSegments()

func buildArmCanonicalSegments() map[string]string {
	canonical := []string{
		// Esqueleto común de cualquier resource ID de ARM.
		"subscriptions",
		"resourceGroups",
		"providers",

		// Namespaces de provider usados en los patrones de ruta.
		"Microsoft.Resources",
		"Microsoft.Storage",
		"Microsoft.Network",
		"Microsoft.Compute",
		"Microsoft.KeyVault",
		"Microsoft.ServiceBus",
		"Microsoft.DocumentDB",

		// Tipos/sub-tipos de recurso (y acciones) usados en los patrones de
		// ruta de cada servicio.
		"storageAccounts",
		"virtualNetworks",
		"subnets",
		"networkInterfaces",
		"disks",
		"virtualMachines",
		"start",
		"powerOff",
		"vaults",
		"namespaces",
		"queues",
		"topics",
		"databaseAccounts",
		"sqlDatabases",
		"containers",
	}

	m := make(map[string]string, len(canonical))
	for _, c := range canonical {
		m[strings.ToLower(c)] = c
	}
	return m
}

// withARMCaseNormalization reescribe r.URL.Path antes de que llegue al mux:
// cualquier segmento cuya versión en minúsculas coincida con un token
// conocido de armCanonicalSegments se reemplaza por su forma canónica. El
// resto de los segmentos (IDs, nombres de recursos elegidos por el
// usuario) se deja exactamente como llegó.
func withARMCaseNormalization(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(r.URL.Path, "/")
		changed := false
		for i, seg := range segments {
			if canon, ok := armCanonicalSegments[strings.ToLower(seg)]; ok && canon != seg {
				segments[i] = canon
				changed = true
			}
		}
		if changed {
			r.URL.Path = strings.Join(segments, "/")
		}
		next.ServeHTTP(w, r)
	})
}
