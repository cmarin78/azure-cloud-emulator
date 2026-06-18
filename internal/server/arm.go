package server

import (
	"net/http"
	"strings"
)

// ResourceID modela el shape estándar de un recurso ARM:
//
//	/subscriptions/{subscriptionID}/resourceGroups/{resourceGroup}/providers/{provider}/{resourceType}/{resourceName}
//
// Todas las APIs de control plane de Azure (Storage, Compute, Key Vault,
// Service Bus, ...) siguen este mismo patrón, así que se parsea una sola
// vez aquí en lugar de repetirlo en cada servicio.
type ResourceID struct {
	SubscriptionID string
	ResourceGroup  string
	Provider       string // p. ej. "Microsoft.Storage"
	ResourceType   string // p. ej. "storageAccounts"
	ResourceName   string
	// SubResourceType/SubResourceName cubren recursos anidados, p. ej.
	// .../storageAccounts/{account}/blobServices/{service}.
	SubResourceType string
	SubResourceName string
}

// ParseResourceID extrae los componentes de un path ARM. Devuelve ok=false
// si el path no sigue el shape esperado.
func ParseResourceID(path string) (ResourceID, bool) {
	parts := splitPath(path)
	id := ResourceID{}

	for len(parts) > 0 {
		if len(parts) < 2 {
			return ResourceID{}, false
		}
		key, val := strings.ToLower(parts[0]), parts[1]
		switch key {
		case "subscriptions":
			id.SubscriptionID = val
		case "resourcegroups":
			id.ResourceGroup = val
		case "providers":
			id.Provider = val
			parts = parts[2:]
			// Lo que sigue a providers/{provider} es resourceType/resourceName
			// y, opcionalmente, subResourceType/subResourceName.
			if len(parts) >= 2 {
				id.ResourceType, id.ResourceName = parts[0], parts[1]
				parts = parts[2:]
			}
			if len(parts) >= 2 {
				id.SubResourceType, id.SubResourceName = parts[0], parts[1]
				parts = parts[2:]
			}
			continue
		default:
			return ResourceID{}, false
		}
		parts = parts[2:]
	}

	if id.SubscriptionID == "" {
		return ResourceID{}, false
	}
	return id, true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// RequireAPIVersion valida que la query string incluya api-version, tal
// como exige ARM en cada llamada. Si falta, escribe la respuesta de error
// estándar y devuelve ok=false; el handler que llama debe retornar de
// inmediato en ese caso.
func RequireAPIVersion(w http.ResponseWriter, r *http.Request) (version string, ok bool) {
	version = r.URL.Query().Get("api-version")
	if version == "" {
		WriteError(w, http.StatusBadRequest, "MissingApiVersionParameter",
			"el parámetro de query 'api-version' es obligatorio")
		return "", false
	}
	return version, true
}
