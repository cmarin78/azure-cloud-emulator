// Package keyvault emula el subconjunto de Microsoft.KeyVault necesario
// para que `az`/Terraform creen un vault y operen secrets/keys/certificates
// dentro de él: vaults (ARM control plane) y secrets/keys/certificates
// (data plane).
//
// Igual que con storage accounts (ver internal/services/storageaccounts),
// el control plane (Microsoft.KeyVault/vaults) y el data plane
// (https://{vault}.vault.azure.net/...) usan protocolos y formas de
// direccionamiento distintos en Azure real. Aquí ambos viven en el mismo
// paquete porque el data plane de Key Vault es mucho más pequeño que el de
// Storage (sin contenedores/colas/tablas anidados, solo tres colecciones
// planas: secrets, keys, certificates), así que no amerita separarlo en
// sub-paquetes.
//
// Las mutaciones de vaults son síncronas (Effort "S" en ROADMAP.md, igual
// que VNets/NICs/disks): a diferencia de storage accounts o VMs, crear un
// vault en Azure real no suele requerir polling explícito en los flujos
// comunes de az CLI/Terraform.
//
// Shape de URLs del data plane soportado (JSON en vez del formato real,
// misma simplificación que blob/queue/table — ver esos paquetes):
//
//	PUT    /{vault}.vault/secrets/{name}       → set secret
//	GET    /{vault}.vault/secrets/{name}       → get secret (última versión)
//	GET    /{vault}.vault/secrets              → list secrets
//	DELETE /{vault}.vault/secrets/{name}       → delete secret
//	PUT    /{vault}.vault/keys/{name}          → create key
//	GET    /{vault}.vault/keys/{name}          → get key
//	GET    /{vault}.vault/keys                 → list keys
//	DELETE /{vault}.vault/keys/{name}          → delete key
//	PUT    /{vault}.vault/certificates/{name}  → create certificate
//	GET    /{vault}.vault/certificates/{name}  → get certificate
//	GET    /{vault}.vault/certificates         → list certificates
//	DELETE /{vault}.vault/certificates/{name}  → delete certificate
package keyvault

import (
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// Service agrupa el estado necesario para atender las rutas de
// Microsoft.KeyVault (vaults ARM + secrets/keys/certificates data-plane).
type Service struct {
	db *storage.DB
}

// New crea el servicio de Key Vault.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Register monta las rutas ARM (control plane) de Microsoft.KeyVault en
// mux. El data plane (secrets/keys/certificates) no se registra aquí: se
// sirve vía ServeHTTP, despachado por el dispatcher compartido en
// cmd/azure-emulator/main.go (mismo patrón que blob/queue/table).
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults",
		s.listVaults)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults/{vaultName}",
		s.putVault)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults/{vaultName}",
		s.getVaultHandler)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults/{vaultName}",
		s.deleteVault)
}

// ServeHTTP atiende una request de data plane (secrets/keys/certificates)
// ya enrutada por el dispatcher compartido. Ver el comentario de
// queue.Service.ServeHTTP para el razonamiento completo de por qué este es
// el único lugar que despacha estas rutas en vez de mux.HandleFunc directo.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountVault := r.PathValue("accountResource")
	vault, ok := strings.CutSuffix(accountVault, ".vault")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{vault}.vault/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	var collection, name string
	if rest != "" {
		parts := strings.SplitN(rest, "/", 2)
		collection = parts[0]
		if len(parts) == 2 {
			name = parts[1]
		}
	}

	switch collection {
	case "secrets":
		s.handleSecrets(w, r, vault, name)
	case "keys":
		s.handleKeys(w, r, vault, name)
	case "certificates":
		s.handleCertificates(w, r, vault, name)
	default:
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta de data plane desconocida: se esperaba 'secrets', 'keys' o 'certificates'")
	}
}
