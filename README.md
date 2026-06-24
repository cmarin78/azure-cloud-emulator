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

All 19 core phases below are complete, plus Phase 20 (real Action Group
webhook delivery), Phase 21 (Logic App Recurrence trigger, with a real
firing goroutine), Phase 23 (Azure SQL Database), and Phase 24 (Azure
Container Registry). See [ROADMAP.md](ROADMAP.md) for what's planned
next (the rest of the behavioral/real-delivery layer inspired by
gcp-emulator's own Phase 11, plus more breadth candidates).

| Phase | Area | What's implemented |
|---|---|---|
| 1 | Core server | HTTP router (logging/recover/CORS middleware), ARM resource-ID parsing, `api-version` validation, async-operation (LRO) helper (`Azure-AsyncOperation`/`Location` polling), embedded BoltDB persistence, `/healthz` |
| 2 | Resource Manager | Fake subscriptions, resource group CRUD (create/update, get, list, async delete) |
| 3 | Storage | Storage account ARM CRUD, blob containers/blobs, queue storage, table storage (all data-plane) |
| 4 | Compute | Virtual networks/subnets, network interfaces, managed disks, static VM image catalog, virtual machines (create/get/delete, start/stop — all async) |
| 5 | Key Vault | Vault ARM CRUD, plus secrets/keys/certificates (data-plane, simulated cryptographic material) |
| 6 | Service Bus + Cosmos DB | Service Bus namespaces (async), queues/topics/subscriptions (sync) + send/peek-lock-receive/complete; Cosmos DB SQL API accounts (async), databases/containers (sync) + document CRUD |
| 7 | Web console | Minimal vanilla-JS console (`web/console`, no build step), served by the binary — resource groups, storage accounts, VMs, Key Vault vaults, Service Bus namespaces, Cosmos DB accounts |
| 8 | az/azurerm compatibility | Fake ARM metadata endpoint, fake Azure AD token issuer, Microsoft Graph stub, `providers` registration endpoint, optional self-signed HTTPS, ARM case-insensitive path matching |
| 9 | Test suite + CI | One `*_test.go` per service package, a `cmd/azure-emulator` registration test (catches route conflicts), GitHub Actions CI |
| 10 | Monitor + Log Analytics | Log Analytics workspaces (+ `sharedKeys`, + Query data-plane stub), action groups, metric alerts (all ARM CRUD, sync) |
| 11 | App Service | App Service Plans, Web Apps (+ start/stop/restart, `config/appsettings` sub-resource) |
| 12 | Networking | NSGs + security rules, Public IP addresses, Load Balancers, Route Tables + routes, Private DNS zones + record sets |
| 13 | AKS | Managed clusters + agent pools (async), `listClusterUserCredential`/`listClusterAdminCredential` — shape-compatible only, no real Kubernetes control plane |
| 14 | Functions | Function definitions, `syncfunctiontriggers`, `host/default/listkeys` — reuses Phase 11's App Service site, no new resource type needed |
| 15 | Entra ID & RBAC | App registrations, service principals, custom role definitions, role assignments (scope-isolated subscription/resource-group storage) — no real directory or authorization evaluation |
| 16 | Managed Identities | User-assigned identities (`Microsoft.ManagedIdentity/userAssignedIdentities`, ARM CRUD, sync), `identity.type=SystemAssigned` sub-object on App Service sites, VMs, and AKS clusters — deterministic fake `tenantId`/`principalId`/`clientId` |
| 17 | Eventing | Event Grid topics + event subscriptions (ARM CRUD, sync; real webhook delivery on publish) and publish (data-plane); Event Hubs namespaces (ARM CRUD, async), event hubs + consumer groups (ARM CRUD, sync), send/receive (data-plane, simplified) |
| 18 | API Management | Service instance (ARM CRUD, async, always succeeds), APIs + operations (ARM CRUD, sync sub-resources), products + subscriptions (ARM CRUD, sync) — deterministic fake gateway URLs and subscription keys, no real proxying/policy evaluation |
| 19 | ARM/Bicep deployments | `Microsoft.Resources/deployments` (ARM CRUD, async): resolves `parameters()`/`variables()`/`resourceId()`, orders resources by `dependsOn`, dispatches each one as a synthetic PUT to its matching existing service; `/operations` (sync), `/validate` (sync, dry-run only) |
| 20 | Action Groups: real webhook delivery | `POST .../actionGroups/{name}/createNotifications` dispatches a real HTTP POST to each `webhookReceivers` entry; result recorded via `lastNotificationTime`/`lastNotificationStatus` (emulator-only fields) |
| 21 | Logic Apps: Recurrence trigger | `Microsoft.Logic/workflows` (ARM CRUD, sync) restricted to a single `Recurrence` trigger + a single `Http` action; a real per-workflow firing goroutine dispatches a real HTTP POST on schedule, plus a synchronous manual `POST .../triggers/{trigger}/run` |
| 23 | Azure SQL Database | `Microsoft.Sql/servers` + `databases` + `firewallRules` (ARM CRUD, sync), plus the singleton/collection sub-resources `azurerm_mssql_server`/`azurerm_mssql_database` poll unconditionally on every refresh: `connectionPolicies` (singleton PUT/GET + a collection GET that satisfies the provider's LRO poller), `restorableDroppedDatabases`, `backupLongTermRetentionPolicies`/`backupShortTermRetentionPolicies`, `securityAlertPolicies`, `transparentDataEncryption` — all read-only fakes with Azure's real "everything disabled" defaults |
| 24 | Azure Container Registry | `Microsoft.ContainerRegistry/registries` (ARM CRUD, sync) plus `checkNameAvailability` (subscription-scoped), `listCredentials` (admin user fake credentials), `replications` (always empty) — `RegistryProperties` includes always-populated `networkRuleSet`/`policies`/`encryption` sub-objects because the real `azurerm` provider dereferences them without a nil check |

### Feature matrix (detail)

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
  namespaces, Cosmos DB accounts, Log Analytics workspaces, action
  groups, metric alerts, App Service plans/sites, virtual networks,
  NSGs, public IPs, load balancers, route tables, private DNS zones,
  AKS managed clusters, and Functions; VMs are list/start/stop/delete
  only).
- ✅ Real `az`/`azurerm` compatibility: ARM metadata discovery, fake AAD
  token issuer, Microsoft Graph stub, `providers` registration, optional
  self-signed TLS, and case-insensitive ARM path matching.
- ✅ Automated test suite (one `*_test.go` per service package, plus a
  `cmd/azure-emulator` registration test) and GitHub Actions CI.
- **Monitor + Log Analytics**: ✅ Log Analytics workspaces (ARM CRUD,
  sync, plus a `sharedKeys` action), ✅ Log Analytics Query data-plane
  stub (always returns an empty result table), ✅ action groups (ARM
  CRUD, sync), ✅ metric alerts (ARM CRUD, sync, referencing an action
  group by id).
- **App Service**: ✅ App Service Plans (ARM CRUD, sync), ✅ Web Apps
  (ARM CRUD, sync; start/stop/restart actions; `config/appsettings`
  StringDictionary sub-resource with full-replace semantics).
- **Networking**: ✅ Network Security Groups + security rules (ARM CRUD,
  sync), ✅ Public IP addresses (ARM CRUD, sync, deterministic fake IP),
  ✅ Load Balancers (ARM CRUD, sync; inline frontend/backend/rule/probe
  collections), ✅ Route Tables + routes (ARM CRUD, sync), ✅ Private DNS
  zones + record sets (ARM CRUD, sync; A/CNAME).
- **AKS**: ✅ managed clusters (ARM CRUD, async; synthesized default
  agent pool, deterministic fake `fqdn`/identity), ✅ agent pools (ARM
  CRUD, async, independently routable sub-resource), ✅
  `listClusterUserCredential`/`listClusterAdminCredential` (sync, fake
  base64 kubeconfig) — shape-compatible only, no real Kubernetes
  control plane.
- **Functions**: ✅ function definitions (ARM CRUD, sync, sub-resource
  of an App Service site), ✅ `syncfunctiontriggers` (sync action, no
  body), ✅ `host/default/listkeys` (sync action, deterministic fake
  `masterKey`/`functionKeys`) — the Function App itself needs no new
  code, it's a `Microsoft.Web/sites` resource already supported by
  Phase 11's App Service implementation.
- **Entra ID & RBAC**: ✅ app registrations (`v1.0/applications`,
  extends the Phase 8 Microsoft Graph stub), ✅ service principals
  (explicit `POST` plus `$filter=appId eq '...'` auto-discovery), ✅
  custom role definitions (`Microsoft.Authorization/roleDefinitions`,
  ARM CRUD, sync), ✅ role assignments
  (`Microsoft.Authorization/roleAssignments`, ARM CRUD, sync,
  scope-isolated subscription/resource-group storage) — no real
  directory or authorization evaluation behind it.
- **Managed Identities**: ✅ user-assigned identities
  (`Microsoft.ManagedIdentity/userAssignedIdentities`, ARM CRUD, sync,
  deterministic fake `tenantId`/`principalId`/`clientId` preserved
  across updates), ✅ `identity.type=SystemAssigned` sub-object on App
  Service sites (Phase 11, including Function Apps via Phase 14),
  virtual machines (Phase 4), and AKS managed clusters (Phase 13) —
  each deterministic per resource, no real directory behind either.
- **Eventing**: ✅ Event Grid topics (ARM CRUD, sync) with a data-plane
  publish endpoint at `{topic}.eventgrid/api/events`, ✅ event
  subscriptions (ARM CRUD, sync; webhook `endpointType`) — publish
  fans out to every subscription and delivers a **real** HTTP POST to
  each webhook destination, recording the outcome via emulator-only
  `lastDeliveryStatus`/`lastDeliveryTime` fields, no retry/
  dead-lettering; ✅ Event Hubs namespaces (ARM CRUD, async), ✅ event
  hubs + consumer groups (ARM CRUD, sync), ✅ send/receive (data-plane,
  simplified flat offset-ordered log — no real partitioning/
  checkpointing).
- **API Management**: ✅ service instance (ARM CRUD, async; always
  succeeds, deterministic fake `gatewayUrl`/`portalUrl`/
  `developerPortalUrl`/`managementApiUrl`/`scmUrl`), ✅ APIs + operations
  (ARM CRUD, sync sub-resources), ✅ products + subscriptions (ARM CRUD,
  sync; deterministic fake `primaryKey`/`secondaryKey`) — deleting the
  service instance cascades over all its APIs/products/subscriptions;
  no real request proxying or policy evaluation.
- **ARM/Bicep deployments**: ✅ `Microsoft.Resources/deployments` (ARM
  CRUD, async) dispatches a submitted template's `resources[]` array as
  synthetic PUTs to each resource's existing service handler, resolving
  `parameters('x')`/`variables('x')`/`resourceId(type, name...)` and
  ordering by `dependsOn` (cycles rejected with 400); ✅
  `/operations` (sync, one entry per dispatched resource); ✅
  `/validate` (sync, resolves/dispatches through the same dry-run path
  that never persists anything) — a failed dispatched resource marks
  the whole deployment `Failed` with a populated error, and deleting a
  deployment removes only its own record, never the resources it
  created; `what-if` is out of scope.
- **Action Groups real webhook delivery (Phase 20)**: ✅
  `POST .../actionGroups/{name}/createNotifications` sends a real HTTP
  POST (Azure common-alert-schema-shaped JSON body) to every
  `webhookReceivers` entry on the action group, recording the outcome
  via emulator-only `lastNotificationTime`/`lastNotificationStatus`
  fields — no retry/dead-lettering, and email/SMS/Azure Function
  receivers remain shape-only with no real delivery.
- **Logic Apps Recurrence trigger (Phase 21)**: ✅
  `Microsoft.Logic/workflows` (ARM CRUD, sync) — `definition.triggers`
  restricted to exactly one `Recurrence`-type trigger and
  `definition.actions` to exactly one `Http`-type action (anything else
  rejected with 400); a real per-workflow goroutine dispatches a real
  HTTP POST to the action's `uri` when the recurrence is due
  (`internal/cronlike`, no retry/dead-lettering, same fire-and-forget
  dispatch convention as Event Grid/Action Groups), resuming
  automatically on server restart for any workflow left `Enabled`; ✅
  `POST .../triggers/{trigger}/run` fires the action **synchronously**
  and reports the outcome immediately via `lastRunStatus`/`lastRunTime`.
- **Azure SQL Database (Phase 23)**: ✅ `Microsoft.Sql/servers` (ARM
  CRUD, sync; `administratorLoginPassword` accepted but never persisted
  or returned, same convention as `compute.OsProfile.AdminPassword`), ✅
  `databases` (ARM CRUD + PATCH, sync, single-level sub-resource), ✅
  `firewallRules` (ARM CRUD, sync). No real query engine — databases are
  ARM records with fake properties (`collation`, `maxSizeBytes`, `sku`).
  `azurerm_mssql_server`/`azurerm_mssql_database` poll several
  unconditional sub-resources on every refresh that Azure real always
  populates, all implemented as read-only fakes: ✅ `connectionPolicies`
  (a PUT/GET singleton named `default`, plus a collection-level GET that
  the provider's LRO poller checks instead of the singleton — without
  it the poller sees a 404 and reports the operation as failed even
  though it actually succeeded), ✅ `restorableDroppedDatabases`
  (always empty, no real backups), ✅
  `backupLongTermRetentionPolicies`/`backupShortTermRetentionPolicies`,
  `securityAlertPolicies`, and `transparentDataEncryption` (each a
  fixed "everything disabled" default matching real Azure's defaults).
  A field-mapping detail worth calling out: the real provider's Read
  populates `sku_name` from `properties.currentServiceObjectiveName`
  and `storage_account_type` from
  `properties.requestedBackupStorageRedundancy` — **not** from the
  top-level `sku.name` or `properties.currentBackupStorageRedundancy`
  one might assume; both fields are populated on every PUT/PATCH to
  match.
- **Azure Container Registry (Phase 24)**: ✅
  `Microsoft.ContainerRegistry/registries` (ARM CRUD, sync;
  `loginServer` deterministically derived as `{name}.azurecr.io`), ✅
  `checkNameAvailability` (subscription-scoped, checks for a name
  collision against existing registries in the same subscription), ✅
  `listCredentials` (deterministic fake `username`/`password`/
  `password2` when `adminUserEnabled=true`), ✅ `replications` (always
  an empty list, no real geo-replication). `RegistryProperties` always
  populates `networkRuleSet`/`policies`/`encryption` sub-objects (with
  "off" values) even though the emulator doesn't enforce any of
  them — the real provider's flatten functions dereference these
  without a nil check and panic if they're absent from the JSON.

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
internal/services/network/      Microsoft.Network/virtualNetworks, subnets, networkInterfaces, networkSecurityGroups, publicIPAddresses, loadBalancers, routeTables, privateDnsZones (ARM CRUD)
internal/services/compute/      Microsoft.Compute/disks, VM image catalog, and virtualMachines (ARM CRUD)
internal/services/keyvault/     Microsoft.KeyVault/vaults (ARM CRUD) + secrets/keys/certificates (path-style {vault}.vault/ data-plane)
internal/services/servicebus/   Microsoft.ServiceBus/namespaces, queues, topics/subscriptions (ARM CRUD) + messaging (path-style {namespace}.servicebus/ data-plane)
internal/services/cosmosdb/     Microsoft.DocumentDB/databaseAccounts, sqlDatabases, containers (ARM CRUD) + documents (path-style {account}.documents/ data-plane)
internal/services/monitor/      Microsoft.OperationalInsights/workspaces + Microsoft.Insights/actionGroups, metricAlerts (ARM CRUD) + Log Analytics Query stub (POST /v1/workspaces/{id}/query)
internal/services/appservice/   Microsoft.Web/serverfarms (App Service Plans) + sites (Web Apps, ARM CRUD) + start/stop/restart actions + config/appsettings sub-resource
internal/services/aks/          Microsoft.ContainerService/managedClusters (ARM CRUD, async) + agentPools sub-resource (ARM CRUD, async) + listClusterUserCredential/listClusterAdminCredential actions
internal/services/functions/    Microsoft.Web/sites/functions sub-resource (ARM CRUD, sync) + syncfunctiontriggers/host/default/listkeys actions (Function App site itself is handled by appservice)
internal/services/armmeta/      fake ARM metadata document (/metadata/endpoints) so az CLI/azurerm can discover this emulator as a custom cloud
internal/services/aadtoken/     fake Azure AD token issuer (/login/{tenant}/oauth2/v2.0/token) accepting any client_id/secret
internal/services/graph/        Microsoft Graph stub: applications + servicePrincipals (POST/GET/$filter auto-discovery) so azurerm can resolve a service principal's object ID
internal/services/authorization/  Microsoft.Authorization/roleDefinitions + roleAssignments (ARM CRUD, sync, scope-isolated subscription/resource-group storage)
internal/services/managedidentity/  Microsoft.ManagedIdentity/userAssignedIdentities (ARM CRUD, sync)
internal/services/eventgrid/    Microsoft.EventGrid/topics + eventSubscriptions (ARM CRUD, sync) + publish (path-style {topic}.eventgrid/ data-plane, real webhook delivery)
internal/services/eventhub/     Microsoft.EventHub/namespaces (ARM CRUD, async) + eventhubs/consumergroups (ARM CRUD, sync) + send/receive (path-style {namespace}.eventhub/ data-plane)
internal/services/apimanagement/  Microsoft.ApiManagement/service (ARM CRUD, async) + apis/operations, products/subscriptions sub-resources (ARM CRUD, sync)
internal/services/deployments/  Microsoft.Resources/deployments (ARM CRUD, async) + operations sub-resource (sync) + validate action — ARM template expression resolver + dependsOn-ordered dispatcher to other services
internal/cronlike/              minimal recurrence evaluator (Recurrence{Frequency, Interval, StartTime}, Validate, Next) for Logic Apps' Recurrence trigger
internal/services/logicapps/    Microsoft.Logic/workflows (ARM CRUD, sync) — single Recurrence trigger + single Http action, real per-workflow firing goroutine, synchronous manual triggers/{trigger}/run action
internal/services/sql/          Microsoft.Sql/servers + databases + firewallRules (ARM CRUD, sync) + connectionPolicies/restorableDroppedDatabases/backup retention policies/securityAlertPolicies/transparentDataEncryption sub-resources
internal/services/containerregistry/  Microsoft.ContainerRegistry/registries (ARM CRUD, sync) + checkNameAvailability/listCredentials/replications actions
internal/devtls/                self-signed TLS certificate generation/caching for the optional -tls flag
web/console/                     minimal vanilla-JS web console (no build step), served by the binary itself
docs/                            banner and other documentation assets
scripts/                         test-az-cli.sh/.ps1 — az rest smoke tests against the emulator; webhook-counter-listener.ps1/.py — minimal HTTP listeners that count real received POSTs, used by the Phase 21 smoke tests to positively confirm webhook delivery
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

Building a local binary with `go build -o bin/azure-emulator
./cmd/azure-emulator` is fine for day-to-day use, but `bin/` is
gitignored — it's only for local builds, never for distribution or
commits.

## Web console

Once the emulator is running, visit `http://localhost:10000/` for a
minimal web console covering resource groups, storage accounts,
virtual machines, Key Vault vaults, Service Bus namespaces, Cosmos DB
accounts, Monitor (Log Analytics workspaces, action groups, metric
alerts), App Service (plans and sites), networking (virtual networks,
NSGs, public IPs, load balancers, route tables, private DNS zones),
AKS managed clusters, and Functions (scoped to whatever's implemented
so far). It's a plain HTML/CSS/JS app with no build step, served by
the binary itself and talking to the emulator's own JSON REST API via
`fetch`.

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

## Running tests

Every service package ships its own `*_test.go` (ARM CRUD plus
data-plane behavior, via `net/http/httptest` — no real network or
external dependencies needed), and `cmd/azure-emulator` has a test
that reproduces `main()`'s exact service-registration order against a
single `http.ServeMux` to catch duplicate-route panics before they'd
hit a real run:

```bash
go build ./...
go vet ./...
go test ./... -race
```

GitHub Actions (`.github/workflows/ci.yml`) runs the same three steps
on every push and pull request.

## Testing with az CLI and Terraform

Every service ships with a way to exercise it from real tooling, not just
`curl`.

### az CLI

az CLI normally expects to discover ARM metadata and authenticate
against real Azure AD before issuing any request. As of Phase 8 the
emulator implements both (`/metadata/endpoints` and a fake token
issuer), and `az cloud register`/`az login --service-principal` against
a `custom` cloud pointed at the emulator gets as far as the token
request — but MSAL's "instance discovery" check rejects `localhost` as
an authority before that, with no flag to disable it in current az CLI
versions. This is a known limitation of the az CLI client itself, not
of the emulator's endpoints (the `azurerm` Terraform provider doesn't
have this problem — see below). Until/unless that's worked around, the
practical way to drive the emulator from az CLI is
[`az rest`](https://learn.microsoft.com/cli/azure/reference-index#az-rest),
which reuses your cached `az login` token (from a real tenant) but lets
you target any URL, including `localhost`:

```bash
az login                       # once, any account/tenant works
./scripts/test-az-cli.sh       # or test-az-cli.ps1 on Windows
```

This exercises, end to end against a running emulator instance:

- **Resource Manager**: subscription auto-vivification, resource group CRUD.
- **Storage**: storage account CRUD; blob container/blob create/upload/
  list/download/delete; queue create/put/peek/get(dequeue)/delete/delete
  queue; table create/insert/get/query/merge/delete entity/delete table.
- **Compute/Network**: virtual network + subnet CRUD, network interface
  CRUD, managed disk CRUD, VM image catalog lookup, virtual machine
  create/get/start/powerOff/delete — confirming the response never
  echoes back `adminPassword`.
- **Key Vault**: vault create/get/list/delete; secret put/get/list (list
  never echoes back `value`)/delete; key put/get/list/delete;
  certificate put/get/list/delete.
- **Service Bus**: namespace create/get/delete; queue create/delete;
  message send/peek-lock-receive/complete; topic + subscription create/
  delete with fan-out send/receive.
- **Cosmos DB**: account create/get/delete; SQL database create/delete;
  container create/delete; document put/get/list/delete.
- **Monitor/Log Analytics**: workspace create/get/list/delete,
  `sharedKeys` action, Log Analytics Query stub, action group create/
  get/list/delete, metric alert create/get/list/delete, and (Phase 20)
  `createNotifications` dispatching a real webhook POST to a local
  test listener — confirmed `lastNotificationStatus: "ok"` on the
  action group afterward.
- **App Service**: App Service Plan create/get/list/delete; Web App
  create/get/list/delete (referencing a plan by id); app settings put/
  get (StringDictionary, full replace); start/stop/restart actions.
- **Networking**: NSG create/get/delete; security rule put/get/delete
  (rejecting an out-of-range priority); Public IP create/get/update/
  delete (deterministic fake IP, preserved across updates); Load
  Balancer create/get/delete (referencing a Public IP by id plus inline
  frontend/backend/rule); Route Table create/get/delete; route put/get/
  delete (rejecting an unrecognized `nextHopType`); Private DNS zone
  create/get/delete plus A record put/get/delete.
- **AKS**: managed cluster create (async)/get (synthesized default
  agent pool + fake `fqdn`)/list; agent pool put (async)/get/list
  (default + the new pool, both reflected on the parent cluster's
  `agentPoolProfiles`); `listClusterUserCredential` (base64 kubeconfig);
  agent pool delete; managed cluster delete cascading over any
  remaining agent pools.
- **Functions**: App Service Plan Y1/Dynamic create; Function App
  create (`kind=functionapp,linux`, reusing the existing `appservice`
  implementation); function definition put/get/list;
  `syncfunctiontriggers` (204, no body); `host/default/listkeys`
  (non-empty `masterKey`); cleanup deletes of the function/app/plan.
- **Entra ID & RBAC**: application create/get; service principal create
  plus `$filter` auto-discovery; role definition put/get/list; role
  assignment put/get at both subscription and resource-group scope —
  confirming a subscription-scope list never includes a
  resource-group-scope assignment; cleanup deletes of the role
  assignments, role definition, service principal, and application.
- **Eventing**: Event Grid topic create/get; event subscription put
  (webhook destination pointed at an unreachable placeholder URL) /get;
  publish (data-plane) followed by a GET confirming a recorded
  delivery attempt (`lastDeliveryStatus`/`lastDeliveryTime`); Event
  Hubs namespace create (async)/get; event hub put/get; consumer group
  put/get; send (data-plane) followed by receive via both the direct
  `{hub}/messages` path and the consumer-group path; cleanup deletes.
- **API Management**: service instance create (async)/get/list; API
  put/get; operation put/get/list; product put; product-API
  association put/get/list; subscription put/get (confirming
  non-empty, deterministic `primaryKey`/`secondaryKey`); cleanup
  deletes in reverse dependency order, including a full
  service-instance delete cascading over its remaining
  APIs/products/subscriptions.
- **ARM/Bicep deployments**: deployment PUT (async) templating a real
  storage account via `parameters()`/`variables()`/`resourceId()`/get
  (`provisioningState: Succeeded`); operations list (one `Succeeded`
  entry); GET on the dispatched storage account confirming it was
  really created; `validate` (shape-only, creates nothing); deployment
  delete; GET on the dispatched storage account again confirming it
  survives the deployment delete; cleanup deletes.
- **Logic Apps (Phase 21)**: workflow PUT/GET with a `Recurrence`
  trigger (`interval: 5` seconds) and an `Http` action pointed at a
  real local counter listener (`scripts/webhook-counter-listener.py`,
  started by the script itself); a manual
  `triggers/recurrence/run` confirming the listener received at least
  1 POST immediately afterward; a 7-second wait confirming the
  listener's counter went up *further* (proving an unprompted,
  automatic recurrence fire actually happened, not just the manual
  one); workflow delete; listener shutdown.

### Terraform (generic `http` provider)

`terraform/smoke-test/` uses the generic `http` provider plus
`local-exec` to verify every REST endpoint responds with the expected
shape, independent of any auth flow:

```bash
cd terraform/smoke-test
terraform init
terraform apply
```

This provisions, against the running emulator: a resource group;
storage account (+ blob container/blob, queue + message, table +
entity); virtual network/subnet/NIC/disk/VM; Key Vault (+ secret/key/
certificate); Service Bus namespace (+ queue + message); Cosmos DB
account (+ database/container/document); a Log Analytics workspace +
action group + metric alert (referencing the action group by id); an
App Service Plan + Web App (referencing the plan by id) + app
settings; Networking (NSG + security rule, Public IP, Load Balancer
referencing the Public IP by id plus inline frontend/backend/rule,
Route Table + route, and a Private DNS zone + A record); an AKS
managed cluster + agent pool; a Functions App Service Plan + Function
App + function definition (plus a `syncfunctiontriggers` call); and
Entra ID & RBAC (an application, a service principal, a custom role
definition, and role assignments at both subscription and
resource-group scope); a user-assigned managed identity; and Eventing
(an Event Grid topic + event subscription with a webhook destination
+ publish, and an Event Hubs namespace + event hub + consumer group +
send); and API Management (a service instance + API + operation +
product + product-API association + subscription). It then reads
each one back via `data "http"` blocks and exposes the parsed JSON as
outputs — including a `role_assignments_sub_list` data source
confirming the subscription-scope list excludes the
resource-group-scope assignment, an `eh_receive_response` data source
confirming a roundtrip send/receive on the event hub, and an
`apim_subscription_response` data source confirming the subscription's
`scope` matches the product's resource ID; and an ARM/Bicep deployment
(a `null_resource`+`local-exec` PUT templating a real storage account
via `parameters()`/`variables()`/`resourceId()`, read back via
`data.http.deployment` and `data.http.deployment_storage_account`); and
a Logic Apps workflow (Phase 21) — `null_resource.logicapps_workflow`
starts a real counter listener (`scripts/webhook-counter-listener.ps1`)
via `Start-Process` and PUTs a workflow with a `Recurrence` trigger +
`Http` action pointed at it, `null_resource.logicapps_trigger_run`
fires a manual trigger run, `null_resource.logicapps_verify` is a
`local-exec` PowerShell script that `throw`s (failing the `apply`) if
the listener's `/count` isn't at least 1 right after the manual run or
doesn't increase further after a 7-second wait (proving a real
automatic recurrence fire), and `null_resource.logicapps_cleanup`
deletes the workflow and stops the listener via a PID file.
Confirmed via a full `apply`/`destroy` cycle against a live emulator
instance.

### Terraform with the real `azurerm` provider

`terraform/azurerm-smoke-test/` points the actual `hashicorp/azurerm`
provider at the emulator (not the generic `http` provider) via
`environment = "custom"` + `metadata_host`, confirming full ARM
metadata discovery, AAD token issuance, service-principal object-ID
resolution (Microsoft Graph), and resource create/destroy all work end
to end:

```bash
go run ./cmd/azure-emulator -tls   # see "Enabling HTTPS" above — required
cd terraform/azurerm-smoke-test
terraform init
terraform apply
terraform destroy
```

This requires the cert-trust steps from "Enabling HTTPS" above (Go's
TLS stack, which `azurerm` uses, reads the OS certificate store). It
provisions and destroys a real `azurerm_resource_group` and a real
`azurerm_resource_group_template_deployment` (templating a storage
account via `parameters()`/`resourceId()`, dispatched through
`internal/services/deployments`), and reads the subscription via
`data "azurerm_subscription"`.

See [`docs/poc-terraform-azurerm.md`](docs/poc-terraform-azurerm.md) for a real, captured `init`/`plan`/`apply`/`destroy` transcript against this provider — including three real bugs this exact test surfaced and fixed in the deployments lifecycle (`exportTemplate`, `properties.providers`, and provider `apiVersions`).

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
