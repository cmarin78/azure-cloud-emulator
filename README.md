<p align="center">
  <img src="docs/banner.svg" alt="Azure Emulator" width="720">
</p>

# Azure Emulator

A local Microsoft Azure emulator written in Go. The goal is to expose REST
APIs compatible with Azure's management and data-plane endpoints (Storage,
Key Vault, Compute, and others), persist everything in a single embedded
file (BoltDB), and ship with a lightweight web console for inspecting
resources — no real Azure subscription, nothing external required.

Same idea as [gcp-emulator](../gcp-emulator), aimed at Azure instead of
GCP: a portable binary (or container) that runs the same way on Windows,
Linux, or macOS, against which you can point the real `az` CLI, the Azure
Terraform provider (`azurerm`), and the official Azure SDKs by overriding
their endpoints to `localhost`.

## Current status

Phase 1 (core server) and Phase 2 (Resource Manager basics) are done:
HTTP router with logging/recover/CORS middleware, ARM resource-ID
parsing, `api-version` validation, an async-operation (LRO) helper
matching `Azure-AsyncOperation`/`Location` polling, embedded BoltDB
persistence, a `/healthz` endpoint, fake subscriptions, and resource
group CRUD (create/update, get, list, async delete). Phase 3 (Storage)
is done: storage account ARM CRUD, blob containers/blobs, queue
storage, and table storage (all data-plane) are implemented. Phase 4
(Compute) is done: virtual networks/subnets, network interfaces,
managed disks, a static VM image catalog, and virtual machines
(create/get/delete, start/stop, all async) are implemented. Phase 5
(Key Vault) is done: vault ARM CRUD plus secrets/keys/certificates
(all data-plane, with simulated cryptographic material) are
implemented. Phase 6 (Service Bus + Cosmos DB) is done: Service Bus
namespaces (ARM, async), queues and topics/subscriptions (ARM, sync)
plus message send/peek-lock-receive/complete (data-plane); Cosmos DB
SQL API accounts (ARM, async), databases and containers (ARM, sync)
plus document CRUD (data-plane). Phase 7 (Web console) is done: a
minimal vanilla-JS console (`web/console`, no build step) is served by
the binary itself for browsing resource groups, storage accounts,
virtual machines, Key Vault vaults, Service Bus namespaces, and Cosmos
DB accounts — see "Web console" below. Phase 8 (real `az`/`azurerm`
compatibility) is done: a fake ARM metadata endpoint
(`/metadata/endpoints`), a fake Azure AD token issuer
(`/login/{tenant}/oauth2/v2.0/token`), a minimal Microsoft Graph stub
(`GET /v1.0/servicePrincipals`), the `providers` registration endpoint,
optional self-signed HTTPS (`-tls`), and a path-normalization
middleware (so ARM's case-insensitive literal segments like
`resourceGroups` match regardless of how a client capitalizes them)
together let the real `azurerm` Terraform provider point at this
emulator directly — see "Testing with az CLI and Terraform" below for
the working end-to-end flow and the one known limitation (az CLI
itself). See [ROADMAP.md](ROADMAP.md) for the next phases.

Planned scope (subject to change as work progresses):

- **Storage**: ✅ storage accounts (ARM CRUD), ✅ blob containers/blobs
  (data-plane: create/list/get/delete containers, upload/download/list/
  delete blobs), ✅ queue storage (data-plane: create/list/get-metadata/
  delete queues, put/peek/get(dequeue)/delete messages, visibility
  timeout + pop receipts), ✅ table storage (data-plane: create/list/
  delete tables, insert/get/query/replace/merge/delete entities with a
  simplified `$filter` subset).
- **Key Vault**: ✅ vaults (ARM CRUD), ✅ secrets (data-plane CRUD), ✅ keys
  (data-plane CRUD, simulated JWK material), ✅ certificates (data-plane
  CRUD, simulated cert material — no real X.509/crypto operations).
- **Compute**: ✅ virtual networks/subnets, ✅ network interfaces, ✅ managed
  disks, ✅ static VM image catalog, ✅ virtual machines (create/get/
  delete, start/stop — all async, matching real Azure's LRO pattern).
- **Resource Manager**: ✅ resource groups, fake subscriptions, ARM-style
  long-running operations.
- **Service Bus**: ✅ namespaces (ARM CRUD, async), ✅ queues and topics/
  subscriptions (ARM CRUD, sync), ✅ message send/peek-lock-receive/
  complete (data-plane, with topic-to-subscriptions fan-out).
- **Cosmos DB**: ✅ SQL API accounts (ARM CRUD, async), ✅ databases and
  containers (ARM CRUD, sync, partition key required), ✅ document CRUD
  (data-plane: put/create/get/list/delete, simplified vs real Azure —
  plain JSON body instead of partition-key headers).
- ✅ Web console for browsing emulated resources (read/list plus create/
  delete for resource groups, storage accounts, vaults, Service Bus
  namespaces, Cosmos DB accounts; VMs are list/start/stop/delete only).
- ✅ Real `az`/`azurerm` compatibility: ARM metadata discovery, fake AAD
  token issuer, Microsoft Graph stub, `providers` registration, optional
  self-signed TLS, and case-insensitive ARM path matching.

## Project structure

```
cmd/azure-emulator/             entry point, wires up storage + server, listens on :10000
internal/storage/               embedded persistence (BoltDB)
internal/server/                router, middlewares, ARM parsing, LRO helper, JSON/error helpers, /healthz
internal/services/resourcemanager/  fake subscriptions + resource group CRUD
internal/services/storageaccounts/  Microsoft.Storage/storageAccounts ARM CRUD (control plane only)
internal/services/blob/         Blob containers/blobs data-plane (path-style {account}.blob/ endpoint)
internal/services/queue/        Queue storage data-plane (path-style {account}.queue/ endpoint)
internal/services/table/        Table storage data-plane (path-style {account}.table/ endpoint)
internal/services/network/      Microsoft.Network/virtualNetworks, subnets, and networkInterfaces (ARM CRUD)
internal/services/compute/      Microsoft.Compute/disks, VM image catalog, and virtualMachines (ARM CRUD)
internal/services/keyvault/     Microsoft.KeyVault/vaults (ARM CRUD) + secrets/keys/certificates (path-style {vault}.vault/ data-plane)
internal/services/servicebus/   Microsoft.ServiceBus/namespaces, queues, topics/subscriptions (ARM CRUD) + messaging (path-style {namespace}.servicebus/ data-plane)
internal/services/cosmosdb/     Microsoft.DocumentDB/databaseAccounts, sqlDatabases, containers (ARM CRUD) + documents (path-style {account}.documents/ data-plane)
internal/services/armmeta/      fake ARM metadata document (/metadata/endpoints) so az CLI/azurerm can discover this emulator as a custom cloud
internal/services/aadtoken/     fake Azure AD token issuer (/login/{tenant}/oauth2/v2.0/token) accepting any client_id/secret
internal/services/graph/        minimal Microsoft Graph stub (GET /v1.0/servicePrincipals) so azurerm can resolve a service principal's object ID
internal/devtls/                self-signed TLS certificate generation/caching for the optional -tls flag
web/console/                     minimal vanilla-JS web console (no build step), served by the binary itself
docs/                            banner and other documentation assets
scripts/                         test-az-cli.sh/.ps1 — az rest smoke tests against the emulator
terraform/smoke-test/            minimal Terraform config exercising the emulator's REST endpoints via the generic http provider
terraform/azurerm-smoke-test/    Terraform config using the real azurerm provider against the emulator (requires -tls)
```

## Requirements

- Go 1.22+ (only needed to build from source — see "Running with Docker"
  below if you'd rather not install it)
- Azure CLI / Terraform (optional, to exercise real commands against the
  emulator once it has endpoints implemented)

> Note: this repo does not bundle the Go toolchain. If you don't have it
> installed, get it from https://go.dev/dl/ (or `winget install GoLang.Go`
> on Windows, `brew install go` on macOS, `apt install golang-go` on
> Linux).

## Running

```bash
cd azure-emulator
go mod tidy
go run ./cmd/azure-emulator
```

By default it listens on `:10000` and persists to
`.azure-emulator-data/azure-emulator.db`. `GET /healthz` confirms the
process is up. Both can be overridden with `-addr`/`-db` flags or the
`AZURE_EMULATOR_ADDR`/`AZURE_EMULATOR_DB` environment variables (the
latter is what the Docker image uses).

## Web console

Once the emulator is running, visit `http://localhost:10000/` for a
minimal web console covering resource groups, storage accounts,
virtual machines, Key Vault vaults, Service Bus namespaces, and Cosmos
DB accounts (scoped to whatever's implemented so far). It's a plain
HTML/CSS/JS app with no build step, served by the binary itself and
talking to the emulator's own JSON REST API via `fetch`.

The console's static files live in `web/console` and are served from
the `-web` flag's directory (default `web/console`; override with
`AZURE_EMULATOR_WEB`). If that directory doesn't exist, the console is
silently disabled and only the REST API is served.

## Enabling HTTPS

Both az CLI and the `azurerm` Terraform provider refuse to treat a
custom cloud as valid over plain HTTP — pointing them at the emulator
for real authentication/ARM calls requires `-tls`:

```bash
go run ./cmd/azure-emulator -tls
```

On first run this generates a self-signed certificate/key pair under
`.azure-emulator-data/tls/` (override with `-tls-cert`/`-tls-key`, or
`AZURE_EMULATOR_TLS_CERT`/`AZURE_EMULATOR_TLS_KEY`) and reuses it on
subsequent runs. Since it's self-signed, both az CLI's Python TLS stack
and `azurerm`'s Go TLS stack need to be told to trust it explicitly:

```powershell
# Windows: trust the cert for both stacks
certutil -addstore Root .azure-emulator-data\tls\cert.pem

# az CLI bundles its own CA list (certifi) instead of using the Windows
# store, so it also needs a combined bundle (its own CAs + this cert):
python -c "import certifi; print(certifi.where())"
Get-Content <certifi-cacert-path>, .azure-emulator-data\tls\cert.pem | Set-Content combined-ca-bundle.pem
$env:REQUESTS_CA_BUNDLE = (Resolve-Path combined-ca-bundle.pem)
```

`azurerm`'s Go HTTP stack uses the OS certificate store directly, so the
`certutil -addstore Root` step alone is enough for Terraform once the
cert is trusted system-wide.

## Running with Docker

```bash
docker compose up --build
```

This builds the image (multi-stage: `golang:1.22-alpine` for compiling,
`alpine:3.20` for the runtime) and starts a container listening on
`localhost:10000`, persisting its BoltDB file to a named volume
(`emulator-data`) so data survives restarts.

Without compose:

```bash
docker build -t azure-emulator:local .
docker run --rm -p 10000:10000 -v emulator-data:/data azure-emulator:local
```

## Testing with az CLI and Terraform

Every service ships with a way to exercise it from real tooling, not just
`curl`.

**az CLI** — az CLI normally expects to discover ARM metadata and
authenticate against real Azure AD before issuing any request. As of
Phase 8 the emulator implements both (`/metadata/endpoints` and a fake
token issuer), and `az cloud register`/`az login --service-principal`
against a `custom` cloud pointed at the emulator gets as far as the
token request — but MSAL's "instance discovery" check rejects
`localhost` as an authority before that, with no flag to disable it in
current az CLI versions. This is a known limitation of the az CLI
client itself, not of the emulator's endpoints (the `azurerm` Terraform
provider doesn't have this problem — see below). Until/unless that's
worked around, the practical way to drive the emulator from az CLI is
[`az rest`](https://learn.microsoft.com/cli/azure/reference-index#az-rest),
which reuses your cached `az login` token (from a real tenant) but lets
you target any URL, including `localhost`:

```bash
az login                       # once, any account/tenant works
./scripts/test-az-cli.sh       # or test-az-cli.ps1 on Windows
```

This exercises subscription auto-vivification, resource group CRUD,
storage account CRUD, blob container/blob CRUD (create container,
upload/list/download/delete blob, delete container), queue storage
(create queue, put/peek/get(dequeue)/delete message, delete queue),
table storage (create table, insert/get/query/merge/delete entity,
delete table), and Compute/Network (virtual network + subnet CRUD,
network interface CRUD, managed disk CRUD, VM image catalog lookup,
virtual machine create/get/start/powerOff/delete — confirming the VM
response never echoes back `adminPassword`), Key Vault (vault
create/get/list/delete, secret put/get/list (list never echoes back
`value`)/delete, key put/get/list/delete, certificate put/get/list/
delete), Service Bus (namespace create/get/delete, queue create/delete,
message send/peek-lock-receive/complete, topic + subscription create/
delete with fan-out send/receive), and Cosmos DB (account create/get/
delete, SQL database create/delete, container create/delete, document
put/get/list/delete) end to end against a running emulator instance.

**Terraform** — `terraform/smoke-test/` uses the generic `http` provider
plus `local-exec` to verify every REST endpoint responds with the
expected shape, independent of any auth flow:

```bash
cd terraform/smoke-test
terraform init
terraform apply
```

This provisions a resource group, storage account (+ blob container/
blob, queue + message, table + entity), virtual network/subnet/NIC/
disk/VM, Key Vault (+ secret/key/certificate), Service Bus namespace
(+ queue + message), and Cosmos DB account (+ database/container/
document) against the running emulator, then reads each one back via
`data "http"` blocks and exposes the parsed JSON as outputs.

**Terraform with the real `azurerm` provider** — `terraform/azurerm-smoke-test/`
points the actual `hashicorp/azurerm` provider at the emulator (not the
generic `http` provider) via `environment = "custom"` +
`metadata_host`, confirming full ARM metadata discovery, AAD token
issuance, service-principal object-ID resolution (Microsoft Graph), and
resource create/destroy all work end to end:

```bash
go run ./cmd/azure-emulator -tls   # see "Enabling HTTPS" above — required
cd terraform/azurerm-smoke-test
terraform init
terraform apply
terraform destroy
```

This requires the cert-trust steps from "Enabling HTTPS" above (Go's
TLS stack, which `azurerm` uses, reads the OS certificate store). It
provisions and destroys a real `azurerm_resource_group`, and reads the
subscription via `data "azurerm_subscription"`.

### Terraform usage examples

Override any default (endpoint, names, location) with `-var` instead of
editing the file:

```bash
terraform apply -var="endpoint=http://localhost:10000" -var="resource_group=my-test-rg" -var="location=westus2"
```

Preview what would change without applying it (useful after editing
`main.tf` or bumping an `api-version`):

```bash
terraform plan
```

Inspect a specific resource's parsed JSON response after `apply` (every
resource created by this smoke test has a matching `<name>_response`
output — see the bottom of `main.tf` for the full list):

```bash
terraform output cosmos_account_response
terraform output sb_message_peek_response
terraform output vm_response
```

Tear everything down — this only destroys Terraform-tracked
`null_resource`/`data` entries (no real Azure billing involved either
way), but it does **not** delete the underlying emulator resources
since those were created via `local-exec` `Invoke-RestMethod` calls,
not real Terraform resources. Use the matching `DELETE` calls in
`scripts/test-az-cli.sh`/`.ps1`, or just restart the emulator with a
fresh `-db` path, to actually clear emulator state:

```bash
terraform destroy
```

Full `azurerm` provider compatibility (fake ARM metadata endpoint, fake
AAD token issuer, Microsoft Graph stub, optional TLS, and ARM
case-insensitive path matching) is implemented as of Phase 8 — see
`terraform/azurerm-smoke-test/` above and [ROADMAP.md](ROADMAP.md) for
details, and "az CLI" above for the one remaining known limitation
(MSAL's instance-discovery check rejecting `localhost`).

## License

MIT — see [LICENSE](LICENSE).
