# Emulator roadmap

List of services/resources to add, ordered by dependencies and value for
real `az` CLI / Terraform (`azurerm`) / SDK workflows. Each phase is
self-contained (it can be merged and used without waiting for the next
one).

Convention: each new service lives in `internal/services/<name>` with its
own `Register(mux)`, following the pattern used by
[gcp-emulator](../gcp-emulator)'s `internal/services/<name>`. Azure's ARM
control plane is consistently shaped
(`/subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}`,
`api-version` query param required on every call, long-running operations
via `Azure-AsyncOperation`/`Location` headers), so `internal/server` should
centralize that parsing/response shaping once rather than re-implementing
it per service — mirroring how gcp-emulator centralizes
`RegisterV2Operations`.

## Current status

Phase 0, Phase 1, and Phase 2 done: core server plus Resource Manager
basics (fake subscriptions, resource group CRUD with async delete,
generic LRO polling) are live, and the project ships as a Docker
image. Phase 3 (Storage) is done: storage account ARM CRUD, blob
containers/blobs, queue storage, and table storage (all data-plane)
are implemented. Phase 4 (Compute) is done: virtual networks/subnets,
network interfaces, managed disks, a static VM image catalog, and
virtual machines (create/get/delete, start/stop) are implemented.
Phase 5 (Key Vault) is done: vault ARM CRUD plus secrets/keys/
certificates (data-plane, simulated cryptographic material) are
implemented. Phase 6 (Messaging and data) is done: Service Bus
namespaces/queues/topics/subscriptions (ARM CRUD) plus message send/
peek-lock-receive/complete (data-plane), and Cosmos DB SQL API
accounts/databases/containers (ARM CRUD) plus document CRUD
(data-plane) are implemented. Phase 7 (Web console) is done. Phase 8
(real `az`/`azurerm` compatibility) is done: fake ARM metadata
discovery, a fake AAD token issuer, a Microsoft Graph stub, the
`providers` registration endpoint, optional self-signed TLS, and an ARM
path case-normalization middleware together let the real `azurerm`
Terraform provider run full `apply`/`destroy` cycles against the
emulator — see Phase 8 below for details and the one remaining known
limitation (az CLI's MSAL instance-discovery check). Phase 9
(automated test suite + CI) is done: every service package has a
`*_test.go` covering its ARM CRUD and data-plane behavior via
`httptest`, `cmd/azure-emulator` has a registration test that
reproduces `main()`'s full wiring to catch duplicate-route panics, and
GitHub Actions runs build/vet/test (with `-race`) on every push and
pull request. Phase 10 (Monitor + Log Analytics) is done: Log
Analytics workspaces (ARM CRUD, sync, plus a `sharedKeys` action and a
data-plane Log Analytics Query stub that always returns an empty
result table), action groups (ARM CRUD, sync), and metric alerts (ARM
CRUD, sync, referencing an action group by id) are implemented. Phase
11 (App Service) is done: App Service Plans (ARM CRUD, sync) and Web
Apps (ARM CRUD, sync, plus start/stop/restart actions and a
`config/appsettings` StringDictionary sub-resource with full-replace
semantics) are implemented. Phase 12 (Networking) is done: Network
Security Groups + security rules, Public IP addresses, Load Balancers
(referencing a public IP and exposing inline frontend/backend/rule
collections), Route Tables + routes, and Private DNS zones + record
sets (A/CNAME) are implemented, all within `internal/services/network`
alongside the existing VNet/subnet/NIC resources. Phase 13 (AKS) is
done: managed clusters (ARM CRUD, async) and agent pools (ARM CRUD,
async, independently routable sub-resource) are implemented — "shape-
compatible, not behavior-complete", since there is no real Kubernetes
control plane behind any of it. Phase 14 (Functions) is done: Function
App definitions (a sub-resource of the existing `Microsoft.Web/sites`
from Phase 11) plus the `syncfunctiontriggers` and `host/default/
listkeys` action routes are implemented. Phase 15 (Entra ID & RBAC) is
done: app registrations and service principals (Microsoft Graph stub,
extending Phase 8) plus custom role definitions and role assignments
(`Microsoft.Authorization`, with scope-isolated subscription/
resource-group storage) are implemented. Phase 16 (Managed Identities)
is done: user-assigned identities (ARM CRUD) plus a system-assigned
`identity` sub-object on App Service sites, VMs, and AKS clusters are
implemented. Phase 17 (Eventing) is done: Event Grid topics + event
subscriptions (with real webhook delivery on publish) and Event Hubs
namespaces/event hubs/consumer groups (with simplified send/receive)
are implemented — see Phase 17 below for details. Phase 18 (API
Management) is done: a service instance (ARM CRUD, async, same
create-async-always-succeeds pattern as AKS) plus APIs/operations and
products/subscriptions (sync sub-resources, with deterministic fake
gateway URLs and subscription keys) are implemented — see Phase 18
below for details. Phase 19 (ARM/Bicep deployments) is done: a
`Microsoft.Resources/deployments` dispatcher that resolves a subset of
ARM template expressions (`parameters()`/`variables()`/`resourceId()`),
orders resources by `dependsOn`, and forwards each one as a synthetic
PUT to the matching existing service is implemented — see Phase 19
below for details. See the table below for the per-phase breakdown.
Phases 20-22 are a newer plan to layer some real *behavior* on top of
specific already-shape-only resources, inspired by a direct review of
gcp-emulator's own Phase 11 ("behavioral logic layer" — real Pub/Sub
push, Cloud Scheduler firing, Cloud Tasks dispatch) and Phase 12
(pluggable real-execution backends) — see those phases for the
rationale and the concrete patterns being ported. Phase 20 (Action
Groups real webhook delivery) is done: `createNotifications` now
dispatches a real HTTP POST to each `webhookReceivers` entry — see
Phase 20 below for details. Phase 21 (Logic App Recurrence trigger) is
done: a new `internal/cronlike` recurrence evaluator plus
`Microsoft.Logic/workflows` (ARM CRUD, sync, a single `Recurrence`
trigger and a single `Http` action that both really work) and a
per-workflow firing goroutine that really dispatches an HTTP call on
schedule — see Phase 21 below for details. Phase 23 (Azure SQL
Database) is done: `Microsoft.Sql/servers`/`databases`/`firewallRules`
(ARM CRUD, sync, plus PATCH support on databases) and the unconditional
sub-resources `azurerm_mssql_server`/`azurerm_mssql_database` poll on
every refresh (`connectionPolicies`, `restorableDroppedDatabases`,
backup retention policies, `securityAlertPolicies`,
`transparentDataEncryption`) are implemented — see Phase 23 below for
details, including a field-mapping bug fix (`sku_name`/
`storage_account_type`) found only via live Terraform testing. Phase 24
(Azure Container Registry) is done: `Microsoft.ContainerRegistry/
registries` (ARM CRUD, sync) plus `checkNameAvailability`,
`listCredentials`, and `replications` are implemented — see Phase 24
below for details.

Note on architecture: path-style data-plane services (blob, queue,
and table) all share the URL shape
`/{account}.{service}/{rest-of-path}`, which `net/http.ServeMux`
treats as the exact same route regardless of wildcard names — so they
can't each register their own pattern. They're wired through a single
shared dispatcher (`registerDataPlane` in `cmd/azure-emulator/main.go`)
that reads the first path segment's suffix and forwards to the right
service's `ServeHTTP`. Any new path-style service must be added as
another case there, not given its own `mux.HandleFunc` call.
Each service is now also covered by an `az rest` smoke test
(`scripts/test-az-cli.sh`/`.ps1`) and a Terraform smoke test
(`terraform/smoke-test/`, using the `http` provider since `azurerm`
needs ARM metadata discovery + real AAD this emulator doesn't
implement yet).

Note on Service Bus/Cosmos DB data-plane: like the path-style services
above, `{namespace}.servicebus/...` and `{account}.documents/...` are
also routed through the same shared `registerDataPlane` dispatcher.
Both also need a small flat existence-index bucket
(`servicebus.dataplane.entities`, `cosmosdb.dataplane.containers`)
kept in sync with ARM CRUD, since data-plane URLs for these services
don't carry subscriptionId/resourceGroup the way the ARM bucket keys
do.

## Phase 0 — Bootstrap ✅ completed

- Repo structure, go.mod, README, banner, license.

## Phase 1 — Core server ✅ completed

| Component | Why | Effort | Status |
|---|---|---|---|
| HTTP router + middleware (`internal/server`) | Foundation for every service | S | done |
| ARM request parsing (subscription/resourceGroup/provider/name from path, `api-version` validation) | Every Azure resource URL follows this shape; centralize once | M | done |
| Long-running operation helper (`Azure-AsyncOperation`, `Location` headers, `PUT` returning 201/202) | `az`/Terraform poll on these for create/delete of most resources | M | done |
| Embedded persistence with BoltDB (`internal/storage`) | Single-file, no external deps — same choice as gcp-emulator | S | done |
| Health/version endpoint | Smoke-test the server is up | S | done |

## Phase 2 — Resource Manager basics ✅ completed

Required before most other services, since every ARM resource is scoped
under a subscription + resource group.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Fake subscriptions (static, accept any GUID) | — | `az`/Terraform need a subscription ID in every URL; no real validation needed | S | done |
| Resource groups (CRUD, async delete via LRO) | subscriptions | `azurerm_resource_group`; almost every other resource references one | S | done |
| Generic long-running operation polling (`operationsStatus/{id}`) | LRO helper (Phase 1) | Needed by Compute/Storage/etc. once they return 202s | S | done |

## Phase 3 — Storage

Mirrors gcp-emulator's GCS, but three sub-APIs instead of one. Independent
of Compute/Key Vault — can be done in parallel with Phase 4/5.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Storage accounts (CRUD, ARM-level) | resource groups | `azurerm_storage_account`; parent of all data-plane endpoints below | M | done |
| Blob containers/blobs (data-plane: create/list/get/delete, upload/download) | storage accounts | Most common Terraform/SDK usage (`azurerm_storage_container`, `azurerm_storage_blob`) | M | done |
| Queue storage (CRUD, enqueue/dequeue) | storage accounts | `azurerm_storage_queue` | S | done |
| Table storage (CRUD, basic entity operations) | storage accounts | `azurerm_storage_table` | S | done |

## Phase 4 — Compute ✅ completed

Depends on Resource Manager (Phase 2) for resource groups, and benefits
from the LRO helper (Phase 1) since every Compute mutation in real Azure
is asynchronous.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Virtual networks + subnets | resource groups | `azurerm_virtual_network`/`azurerm_subnet`; required by NICs | S | done |
| Network interfaces (NICs) | virtual networks | `azurerm_network_interface`; required by VMs | S | done |
| Managed disks | resource groups | `azurerm_managed_disk`; required by VM OS disk | S | done |
| VM images (static catalog, e.g. Ubuntu 22.04, Windows Server 2022) | — | Required by VM image reference | S | done |
| Virtual machines (create/get/delete, start/stop) | NICs, disks, images | `azurerm_linux_virtual_machine`/`azurerm_windows_virtual_machine` | L | done |

Each resource is covered by an `az rest` smoke test
(`scripts/test-az-cli.sh`/`.ps1`) and a Terraform smoke test
(`terraform/smoke-test/`, via the `http` provider — see the note above
on why `azurerm` itself can't point at the emulator yet). Confirmed:
VNet/subnet/NIC/disk CRUD, image catalog resolution (including
`version: "latest"`), VM create (202 + body)/get (no `adminPassword`
leaked)/start/powerOff (202, no body)/delete (202), and resource group
cascade cleanup via `az rest`.

Target (still open): `terraform apply`/`destroy` against the real
`azurerm_virtual_network` + `azurerm_network_interface` +
`azurerm_linux_virtual_machine` resources, without provider patches —
blocked on the fake ARM metadata/AAD work tracked below, same as the
`azurerm_storage_account` equivalent in Phase 3.

## Phase 5 — Key Vault ✅ completed

Standalone, no dependency on Compute/Storage beyond a resource group.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Vaults (ARM-level CRUD) | resource groups | `azurerm_key_vault` | S | done |
| Secrets (CRUD) | vaults | `azurerm_key_vault_secret`; most common use case | S | done |
| Keys (CRUD, basic ops) | vaults | `azurerm_key_vault_key` | M | done |
| Certificates (CRUD, basic ops) | vaults | `azurerm_key_vault_certificate` | M | done |

Keys and certificates don't implement real cryptographic operations
(sign/encrypt/wrapKey/X.509) — they generate random bytes via
`crypto/rand` to populate JWK fields (`n`/`e`) or certificate fields
(`cer`/`x5t` thumbprint), which is enough for `az`/Terraform to
create/read/list/delete them end to end. Secrets follow the same
list-never-echoes-the-value convention as the real API. Vault deletes
are idempotent (204 if missing, matching resource groups' convention);
secret/key/certificate deletes return 404 if missing (matching queue
storage's data-plane convention).

## Phase 6 — Messaging and data ✅ completed

Independent of each other; each is a new package similar in size to
Storage.

| Service | Minimum resources | Effort | Status |
|---|---|---|---|
| Service Bus | namespaces, queues, topics/subscriptions, send/receive | M | done |
| Cosmos DB (SQL API) | accounts, databases, containers, document CRUD | L | done |

Service Bus: namespace create/get/delete is async (matching real
Azure's LRO pattern), queues and topics/subscriptions are sync ARM
sub-resources. Messaging uses peek-lock semantics (`lockToken`,
`lockedUntilUtc`, `deliveryCount`), with `?peeklock=false` for
peek-only reads and `?maxmessages=`/`?locktimeout=` query params;
sending to a topic fans out to all of its subscriptions; completing a
message requires an exact `lockToken` match via
`DELETE .../messages/{id}?lockToken=...`.

Cosmos DB: account create/get/delete is async, databases and
containers are sync ARM sub-resources (container creation requires
`partitionKey.paths`). Document data-plane is simplified vs real Azure
— a plain JSON body instead of the `x-ms-documentdb-partitionkey`
header — and supports PUT (create/replace by id in the URL), POST
(create with an auto-generated id if the body omits one), GET (single
document or list), and DELETE (404 if missing, matching Key Vault's
data-plane convention rather than resource groups' idempotent 204).

## Phase 7 — Web console ✅ completed

| Component | Note | Effort | Status |
|---|---|---|---|
| Minimal UI (`web/console`) | Browse resource groups, storage, VMs, vaults, Service Bus namespaces, Cosmos DB accounts | M | done |

Plain vanilla HTML/CSS/JS, no build step — the binary itself serves it
via `http.FileServer`, talking to the emulator's own JSON REST API with
`fetch` (same origin). Controlled by the `-web` flag
(`AZURE_EMULATOR_WEB` env var), default `web/console`; the console is
disabled automatically if that directory doesn't exist. Covers all six
ARM resource types implemented through Phase 6: resource groups,
storage accounts, virtual machines (list/start/stop/delete only — VM
creation needs a pre-existing NIC/disk, out of scope for the console),
Key Vault vaults, Service Bus namespaces, and Cosmos DB accounts.

Note on routing: the console's static assets are served under
`/console/` rather than at the root. The data-plane dispatcher
(`registerDataPlane`) registers `/{accountResource}/{path...}`, which
`net/http.ServeMux` treats as a "subtree" pattern — a request for a
single top-level path like `/style.css` (no trailing slash) gets
redirected to `/style.css/` first, and that second request matches the
wildcard (`accountResource="style.css"`, empty `path`), landing in the
dispatcher's default case as a 404. A literal prefix like `/console/`
is more specific than the wildcard, so `ServeMux` picks it first and
the redirect/404 never happens. Only `GET /` itself is registered as an
exact pattern (`GET /{$}`) serving `index.html` directly.

## Phase 8 — Real `az`/`azurerm` compatibility ✅ completed

| Component | Why | Effort | Status |
|---|---|---|---|
| Fake ARM metadata endpoint (`/metadata/endpoints`, `internal/services/armmeta`) | `az cloud register` and `azurerm`'s `environment = "custom"` both require this to discover endpoint URLs before issuing any other request | M | done |
| Fake Azure AD token issuer (`/login/{tenant}/oauth2/v2.0/token`, `internal/services/aadtoken`) | Accepts any `client_id`/`client_secret`/`tenant_id` and issues a usable bearer token, standing in for real AAD | M | done |
| `providers` registration endpoint (`internal/services/resourcemanager`) | `azurerm` checks `GET /subscriptions/{id}/providers[/{namespace}]` at startup; an unregistered namespace fails the plan before any resource call | S | done |
| Minimal Microsoft Graph stub (`GET /v1.0/servicePrincipals`, `internal/services/graph`) | `azurerm` resolves the authenticated service principal's object ID via Graph when the access token has no `oid` claim — always true here, since the fake token issuer doesn't simulate a real directory | S | done |
| Optional self-signed TLS (`-tls`/`-tls-cert`/`-tls-key`, `internal/devtls`) | Both az CLI's MSAL stack and `azurerm`'s Go TLS stack refuse to treat a custom cloud as valid over plain HTTP | M | done |
| ARM path case-normalization middleware (`internal/server/armcase.go`) | `azurerm`'s Go SDK normalizes the fixed segments of resource IDs it builds to lowercase (e.g. `resourcegroups`), but `net/http.ServeMux` route patterns match literal segments case-sensitively (`resourceGroups`) | M | done |
| `GET /subscriptions/{id}/resourceGroups/{rg}/resources` (generic resource listing, `resourcemanager.go`) | `azurerm` calls this before `terraform destroy` of a resource group to determine deletion ordering of its contents; returns an empty list since this emulator has no cross-service resource index | S | done |
| `terraform/azurerm-smoke-test/` | Proves the real `hashicorp/azurerm` provider (not the generic `http` provider) works end to end: auth, metadata discovery, resource create, resource destroy | S | done |

Confirmed via two full `terraform apply`/`destroy` cycles against the
real `azurerm` provider (`azurerm_resource_group` create + destroy,
plus `data "azurerm_subscription"` read) — see "Testing with az CLI and
Terraform" in README.md for the exact commands and required cert-trust
steps.

Known limitation (not fixable from the emulator side): az CLI's MSAL
library performs an "instance discovery" check against Microsoft's own
endpoint before accepting any authority, and rejects `localhost` with
no documented flag to disable this in the currently-installed az CLI
version. This blocks `az login --service-principal`/`az cloud register`
specifically — `az rest` (reusing a real `az login` token) remains the
practical way to drive the emulator from az CLI. `azurerm`'s Go-based
auth stack does not perform this check, so Terraform is unaffected.

## Phase 9 — Automated test suite + CI ✅ completed

| Component | Why | Effort | Status |
|---|---|---|---|
| `internal/testutil` (`NewDB`, `DoJSON`, `WithAPIVersion`) | Shared helper for spinning up an in-memory BoltDB + driving JSON requests against an `httptest.Server`, avoiding per-package boilerplate | S | done |
| `*_test.go` per service package (resourcemanager, storageaccounts, blob, queue, table, network, compute, keyvault, servicebus, cosmosdb, armmeta, aadtoken, graph) | Regression coverage for ARM CRUD (idempotent vs. non-idempotent deletes, required-field validation, parent-resource existence checks) and data-plane behavior (blob/queue/table operations, Key Vault secrets/keys/certs, Service Bus send/peek-lock/complete/fan-out, Cosmos DB documents) | L | done |
| `cmd/azure-emulator/main_test.go` (`TestAllServicesRegisterWithoutPanic`) | Reproduces `main()`'s exact service-registration order against a single `http.ServeMux`, so a duplicate route pattern panics in CI instead of at first real startup — same rationale as gcp-emulator's equivalent test | S | done |
| `.github/workflows/ci.yml` | Runs `go build ./...`, `go vet ./...`, and `go test ./... -race` on every push/PR | S | done |

This phase mirrors gcp-emulator's own post-launch addition of a test
suite. Writing the Service Bus test (`TestTopicFanOutToSubscriptions`)
caught a real bug: `queueEntityPath`/`topicEntityPath` in
`internal/services/servicebus/dataplane_index.go` both produced the
identical key format (`namespace + "/" + name`), so any topic was also
misidentified as a queue by the messaging dispatcher's `isQueue`
check (checked before `isTopic`) — meaning topics always took the
single-recipient queue send path instead of fanning out to their
subscriptions. Fixed by giving queues and topics distinct key prefixes
(`"/queues/"` vs. `"/topics/"`); confirmed via `grep` that no other
code duplicated the old format, so the fix was self-contained to that
one file.

## Phase 10 — Monitor + Log Analytics ✅ completed

Standalone, no dependency on Compute/Storage beyond a resource group
(metric alerts reference a resource ID as a "scope" but the emulator
never validates that the referenced resource actually exists).

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Log Analytics workspaces (ARM CRUD, sync, plus `sharedKeys` action) | resource groups | `azurerm_log_analytics_workspace`; required by most other Monitor/diagnostics resources in real Azure | S | done |
| Log Analytics Query stub (data-plane, `POST /v1/workspaces/{workspaceId}/query`) | workspaces | Lets SDKs/tools that issue KQL queries against a workspace get a well-shaped (always-empty) response instead of a connection error | S | done |
| Action groups (ARM CRUD, sync) | resource groups | `azurerm_monitor_action_group`; referenced by metric alerts | S | done |
| Metric alerts (ARM CRUD, sync) | action groups | `azurerm_monitor_metric_alert` | S | done |

All four are synchronous (no LRO), matching real Azure for this
resource family. Workspace defaults: `sku.name = "PerGB2018"`,
`retentionInDays = 30`, a deterministic per-workspace `customerId`
(stable across updates). `sharedKeys` mirrors the start/stop
action-route pattern already used by `compute/vms.go`. `criteria` and
`actions` on metric alerts, and `emailReceivers` on action groups, are
persisted as raw JSON (`json.RawMessage`) without ever being evaluated
— there is no real metrics pipeline behind this, the same way
gcp-emulator never evaluates `monitoring.AlertPolicy.Conditions`.
Workspace/action-group/metric-alert deletes are idempotent (204 if
missing, matching resource groups' convention). Confirmed via
`monitor_test.go` (`httptest`), the `az rest` smoke tests
(`scripts/test-az-cli.sh`/`.ps1`), and a real `terraform apply`/
`destroy` cycle (`terraform/smoke-test/main.tf`, via the `http`
provider).

## Phase 11 — App Service ✅ completed

Standalone, no dependency on Compute/Storage beyond a resource group
(Web Apps reference a plan by `serverFarmId` but the emulator never
validates that the referenced plan actually exists, same
no-referential-integrity approach as Monitor's action-group reference).

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| App Service Plans (ARM CRUD, sync) | resource groups | `azurerm_service_plan`; required by every Web App | S | done |
| Web Apps (ARM CRUD, sync; start/stop/restart actions) | App Service Plans | `azurerm_linux_web_app`/`azurerm_windows_web_app` | S | done |
| `config/appsettings` sub-resource (StringDictionary, full replace) | Web Apps | `azurerm_linux_web_app`'s `app_settings` block writes this sub-resource directly | S | done |

Both resources are fully synchronous (no LRO), matching real Azure for
this resource family. Plan defaults: `sku.capacity = 1` if omitted;
`reserved` is derived from `kind` (`true` for any `kind` containing
`linux`). Site defaults: `state = "Running"`, `enabled = true` on
create; `defaultHostName` is a deterministic
`{name}.azurewebsites.net` (same fake-but-stable derivation pattern as
Monitor's per-workspace `customerId`). `start`/`stop`/`restart` mutate
only `properties.state`, mirroring the action-route pattern already
used by `compute/vms.go` and Monitor's `sharedKeys`. `config/
appsettings` auto-vivifies to an empty dictionary on GET before any
PUT, and each PUT fully replaces the dictionary rather than merging
(matching `azurerm_linux_web_app`'s `app_settings` apply behavior —
confirmed via `appservice_test.go`, where an initial test failure
turned out to be a test-harness artifact from reusing a populated
`map[string]string` across two decodes, not an actual service bug;
the storage layer (`storage.DB.Put`) and the production handler
(`putAppSettings`) were both confirmed to do a true full replace).
Plan/site deletes are idempotent (204 if missing, matching resource
groups' convention); deleting a site also cascades to its app
settings bucket entry. Confirmed via `appservice_test.go` (`httptest`),
the `az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1`), and a real
`terraform apply`/`destroy` cycle (`terraform/smoke-test/main.tf`, via
the `http` provider).

## Phase 12 — Networking ✅ completed

Standalone, all within `internal/services/network` alongside the
existing VNet/subnet/NIC resources from Phase 4. Subnets gained two
optional reference fields (`networkSecurityGroup`, `routeTable`), with
no referential integrity enforced — same no-validation convention
used for Monitor's action-group reference and App Service's
`serverFarmId`.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Network Security Groups + security rules (ARM CRUD, sync) | resource groups | `azurerm_network_security_group`/`azurerm_network_security_rule`; security rules are a full ARM sub-resource, independently routable | S | done |
| Public IP addresses (ARM CRUD, sync) | resource groups | `azurerm_public_ip`; required by Load Balancer frontends | S | done |
| Load Balancers (ARM CRUD, sync) | Public IP addresses | `azurerm_lb`/`azurerm_lb_backend_address_pool`/`azurerm_lb_rule`; frontend/backend/rule/probe collections are managed inline on the parent resource, not as independent sub-resource routes | M | done |
| Route Tables + routes (ARM CRUD, sync) | resource groups | `azurerm_route_table`/`azurerm_route`; routes are a full ARM sub-resource, independently routable | S | done |
| Private DNS zones + record sets (ARM CRUD, sync) | resource groups | `azurerm_private_dns_zone`/`azurerm_private_dns_a_record`/`azurerm_private_dns_cname_record`; record sets use a path-wildcard `{recordType}` segment (A, CNAME) | M | done |

All five are fully synchronous (no LRO), matching the rest of the
Networking family. Security rules reject priorities outside Azure's
valid range (100-4096) with 400; routes reject an unrecognized
`nextHopType` with 400. Public IP addresses get a deterministic fake
`ipAddress` (FNV-32a hash of the resource's full ARM ID, formatted as
`20.b2.b3.b4`) assigned on first PUT and preserved across updates,
mirroring the per-resource deterministic-fake-data pattern already
used by Monitor's `customerId` and App Service's `defaultHostName`.
Load Balancer frontend/backend/rule/probe collections are fully
replaced inline on every PUT of the parent resource — `azurerm`
manages them that way too, so no independent sub-resource routes are
exposed for them (unlike NSG security rules and Route Table routes,
which `azurerm` manages as separate resources and which this emulator
therefore exposes as separate, independently routable ARM
sub-resources). Private DNS zones force `location = "global"`
server-side regardless of what the request body sends, matching real
Azure; `numberOfRecordSets` is computed from the zone's actual record
sets rather than tracked separately. NSG and Private DNS zone deletes
are idempotent (204 if missing, matching resource groups' convention).
Confirmed via `phase12_test.go` (`httptest`, covering all five
resources plus the Subnet→NSG/RouteTable reference behavior), the
`az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1`), and a real
`terraform apply`/`destroy` cycle (`terraform/smoke-test/main.tf`, via
the `http` provider).

## Phase 13 — AKS ✅ completed

Standalone, no dependency on Compute/Networking beyond a resource group
(in real Azure a cluster references a VNet subnet for its nodes, but
this emulator never validates that the referenced subnet actually
exists, same no-referential-integrity approach used throughout —
Monitor's action-group reference, App Service's `serverFarmId`, etc.).

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Managed clusters (ARM CRUD, async) | resource groups | `azurerm_kubernetes_cluster`; the cluster itself, plus a synthesized "default" System agent pool from `properties.agentPoolProfiles` on create | M | done |
| Agent pools (ARM CRUD, async, independently routable sub-resource) | managed clusters | `azurerm_kubernetes_cluster_node_pool`; any node pool beyond the cluster's inline `default_node_pool` | S | done |
| `listClusterUserCredential`/`listClusterAdminCredential` (sync action, data-plane-shaped) | managed clusters | `az aks get-credentials`/`azurerm`'s `kube_config` attribute both expect a base64 kubeconfig back | S | done |

Both managed clusters and agent pools are asynchronous (LRO), matching
real Azure's AKS control-plane behavior, unlike most other resource
families in this emulator which are synchronous. There is no real
Kubernetes control plane behind any of this — `provisioningState` and
`powerState` always report `"Succeeded"`/`"Running"`, matching the
project's "shape-compatible, not behavior-complete" philosophy already
used for Key Vault's simulated cryptographic material and Monitor's
unevaluated alert criteria. `fqdn` and the identity's `principalId`/
`tenantId` are deterministic per-cluster fake values (FNV-32a hash of
the cluster's resource ID, same derivation pattern as Public IP's fake
`ipAddress` in Phase 12), so they stay stable across repeated GETs.
Creating a cluster synthesizes a "default" System-mode agent pool from
`properties.agentPoolProfiles` and immediately persists it as an
independently routable `agentPools` sub-resource, so `GET .../
agentPools/default` succeeds without a separate PUT — mirroring how
`az aks create --node-count` exposes its default pool via `az aks
nodepool show` from the start. `agentPoolProfiles` on the parent
cluster is always read fresh from the agent-pool sub-resource bucket
on GET, so adding/removing pools via the sub-resource route is
reflected immediately. `listClusterUserCredential`/
`listClusterAdminCredential` return a synchronous 200 with a
base64-encoded fake kubeconfig YAML (server URL derived from the fake
`fqdn`, a fake bearer token) — no LRO headers, same sync-action-route
pattern already used by Monitor's `sharedKeys` and App Service's
start/stop/restart. Agent pool PUT/DELETE require the parent cluster
to exist first (404 otherwise); deleting a cluster cascades
synchronously to all of its agent pools before returning the async
delete response, matching real Azure's "deleting a cluster deletes its
node pools" behavior. Cluster/agent-pool deletes are idempotent (204
if missing, matching resource groups' convention). Confirmed via
`aks_test.go` (`httptest`, covering cluster/pool lifecycle, dnsPrefix
and agent-pool-mode validation, and missing-parent-cluster 404), the
`az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1`), and a real
`terraform apply`/`destroy` cycle (`terraform/smoke-test/main.tf`, via
the `http` provider).

## Phase 14 — Functions ✅ completed

Standalone, no dependency on Compute/Networking beyond a resource group.
A Function App **is** a `Microsoft.Web/sites` resource (`kind=
"functionapp"`/`"functionapp,linux"`) — already fully supported by
`appservice.putSite` from Phase 11 without any changes. This phase only
adds the `Microsoft.Web/sites/functions` sub-resource plus two action
routes, both living in their own `internal/services/functions` package.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Function definitions (ARM CRUD, sync, sub-resource of a site) | App Service sites (Phase 11) | `azurerm_function_app_function`; one routable resource per function in the app | S | done |
| `syncfunctiontriggers` (sync action, no body) | Function App site | `az functionapp` and Terraform call this after deploying code; the emulator has no real deploy step, so it's a no-op 204 | S | done |
| `host/default/listkeys` (sync action, data-plane-shaped) | Function App site | `azurerm`'s `default_hostname`-adjacent key lookups and `az functionapp keys list` both expect a `masterKey`/`functionKeys` JSON body back | S | done |

Function definitions are fully synchronous (no LRO), matching App
Service's site/plan resources from Phase 11. `language` and
`config.bindings` are persisted as-is from the request body without
any validation — same "shape-compatible, not behavior-complete"
approach as Key Vault's simulated cryptographic material and AKS's
unevaluated `provisioningState`. `invoke_url_template` is derived
deterministically from the parent site's `defaultHostName` (itself
deterministic per Phase 11) plus the function's name, so it stays
stable across repeated GETs without being persisted separately.
Neither function definitions nor the App Service Plan/site they
reference are validated for existence — same no-referential-integrity
convention used throughout (Monitor's action-group reference, App
Service's `serverFarmId`, AKS's subnet reference). `host/default/
listkeys` returns a `masterKey` and a `functionKeys.default` entry,
both deterministic fake values (FNV-32a hash of the site's resource
ID, same derivation pattern as Public IP's fake `ipAddress` in Phase
12 and AKS's fake `fqdn`/identity in Phase 13) rather than random, so
repeated calls return the same keys. Function definition deletes are
idempotent (204 if missing, matching resource groups' convention).
Confirmed via `functions_test.go` (`httptest`, covering function
definition CRUD, `syncfunctiontriggers`, `listkeys`, and that an
App Service Plan/site created via the existing `appservice` package
needs no changes to host a Function App), the `az rest` smoke tests
(`scripts/test-az-cli.sh`/`.ps1` — App Service Plan Y1/Dynamic create
→ Function App create → function definition put/get/list →
`syncfunctiontriggers` (204) → `host/default/listkeys` (non-empty
`masterKey`) → cleanup deletes), and a real `terraform apply`/
`destroy` cycle (`terraform/smoke-test/main.tf`, via the `http`
provider).

## Phase 15 — Entra ID (Azure AD) & RBAC ✅ completed

Standalone, but this is the phase most other future phases will lean
on: managed identities (Phase 16) need a principal to assign roles to,
and any resource's `identity` block needs a directory object behind
it. Builds directly on top of the existing `internal/services/graph`
(Microsoft Graph stub) and `internal/services/aadtoken` (fake AAD
token issuer) from Phase 8, rather than starting a new package from
scratch where it overlaps.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| App registrations (`Microsoft.Graph` `applications`, data-plane-shaped) | `graph` package (Phase 8) | `azuread_application`; the directory-side representation behind every service principal | S | done |
| Service principals (extend the existing `servicePrincipals` stub to support create, not just list) | App registrations | `azuread_service_principal`; currently the Phase 8 stub only supports `GET /v1.0/servicePrincipals` for `azurerm`'s own auth bootstrap | S | done |
| Custom role definitions (`Microsoft.Authorization/roleDefinitions`, ARM CRUD, sync) | resource groups (any scope) | `azurerm_role_definition`; defines a named set of `actions`/`notActions`, no real permission evaluation | S | done |
| Role assignments (`Microsoft.Authorization/roleAssignments`, ARM CRUD, sync) | role definitions, a principal (service principal or managed identity) | `azurerm_role_assignment`; links a principal to a role at a scope — same no-referential-integrity convention as the rest of the project, so the principal/role don't need to actually exist | S | done |

Like Key Vault's simulated cryptographic material, none of this
performs real authorization — `roleAssignments` just persists the
link; nothing in the emulator ever checks it before allowing a
request. This is enough for `azurerm`/`azuread` Terraform configs that
provision RBAC alongside real resources to apply and destroy cleanly
against the emulator.

Applications and service principals live in `internal/services/graph`
(extending the Phase 8 stub), registered as literal `/v1.0/`-prefixed
routes (`applications[/{id}]`, `servicePrincipals[/{id}]`) so they stay
more specific than the shared data-plane dispatcher's wildcard, the
same conflict class previously resolved for `aadtoken`'s `/login/`
prefix. There is no real directory behind any of it: any request body
is accepted, `displayName` isn't required to be unique, and object IDs
are either a random GUID (explicit `POST`) or a deterministic
sha256-derived value (the pre-existing auto-discovery-by-`appId`
lookup path that `azurerm`'s own auth bootstrap already relied on since
Phase 8). Role definitions and role assignments live in a new
`internal/services/authorization` package, registered under
`Microsoft.Authorization` (locations `["global"]`); both are fully
synchronous (no LRO), matching the rest of the no-real-evaluation
resource families in this emulator. Role assignments use a two-bucket
storage split — subscription-scope and resource-group-scope
assignments are kept in separate BoltDB buckets
(`authorization.roleassignments.subscription`/`.resourcegroup`) — so
that a subscription-scope `LIST` can never accidentally include a
resource-group-scope assignment whose subscription ID happens to match
as a string prefix. Confirmed via `authorization_test.go` (`httptest`,
including a dedicated `TestRoleAssignmentResourceGroupScope` proving
the bucket-split isolation), the `az rest` smoke tests
(`scripts/test-az-cli.sh`/`.ps1` — app registration → service principal
→ auto-discovery `$filter` lookup → role definition CRUD → role
assignment create at both scopes → cleanup), and a real `terraform
apply`/`destroy` cycle (`terraform/smoke-test/main.tf`, via the `http`
provider, including a `data.http.role_assignments_sub_list` check that
re-verifies the same scope-isolation behavior from Terraform's side).

## Phase 16 — Managed Identities ✅ completed

Depended conceptually on Phase 15 (a managed identity is a kind of
service principal), but ended up implementable independently — the
`identity` sub-object pattern below doesn't require role assignments
to exist.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| User-assigned identities (`Microsoft.ManagedIdentity/userAssignedIdentities`, ARM CRUD, sync) | resource groups | `azurerm_user_assigned_identity`; a standalone identity resource other resources reference by id | S | done |
| System-assigned `identity` sub-object on existing resources (App Service sites, VMs, AKS clusters) | the resource it's attached to | `azurerm_linux_web_app`'s/`azurerm_linux_virtual_machine`'s/etc. `identity { type = "SystemAssigned" }` block expects a `principalId`/`tenantId` back on the parent resource, deterministic per resource like AKS's existing identity fields (Phase 13) | M | done |

Implemented in a new `internal/services/managedidentity` package
(`Microsoft.ManagedIdentity`, registered in `resourcemanager.go`'s
`registeredNamespaces` and wired in `cmd/azure-emulator/main.go`).
User-assigned identities are fully synchronous (no LRO), matching the
rest of the no-real-evaluation resource families in this emulator;
deletes are idempotent (200 if it existed, 204 if not, matching
resource groups' convention). System-assigned identities reuse the
same deterministic-fake-value derivation already used for AKS's
`identity.principalId`/`tenantId` (Phase 13) and Public IP's fake
address (Phase 12) — an FNV-32a hash of the parent resource's full ARM
ID plus a distinguishing suffix (`-tenant`/`-principal`/`-client`), so
both `tenantId`/`principalId`/`clientId` on a user-assigned identity
and `principalId`/`tenantId` on a system-assigned `identity` block are
stable across repeated GETs and preserved across updates (confirmed
live: two successive PUTs of the same user-assigned identity return
the same `principalId`). No new package needed for the sub-object
part — each of `appservice` (Web Apps/Function App sites), `compute`
(VMs), and `aks` (managed clusters) keeps its own small `Identity`
struct and derives the same way, rather than sharing one type across
packages; Function Apps get this for free since a Function App **is**
an `appservice` site (Phase 14). Confirmed via `managedidentity_test.go`
and the existing `appservice_test.go`/`compute_test.go`/`aks_test.go`
(`httptest`), the `az rest` smoke tests (`scripts/test-az-cli.sh`/
`.ps1` — identity PUT/GET/LIST, a supporting VNet/subnet/NIC, a VM
created with `identity.type=SystemAssigned`, GET confirming non-empty
`principalId`/`tenantId`, then cleanup deletes in reverse dependency
order including a DELETE-twice idempotency check on the identity), and
a real `terraform apply`/`destroy` cycle
(`terraform/smoke-test/main.tf`, via the `http` provider, with a
`data.http.user_assigned_identity` read-back check).

## Phase 17 — Eventing (Event Grid + Event Hubs) ✅ completed

Independent of everything above; sized similarly to Service Bus
(Phase 6).

| Service | Minimum resources | Effort | Status |
|---|---|---|---|
| Event Grid | topics (ARM CRUD, sync), event subscriptions (ARM CRUD, sync, webhook endpoint reference), publish (data-plane, sync) — **real webhook delivery on publish** | M | done |
| Event Hubs | namespaces (ARM CRUD, async), event hubs + consumer groups (ARM CRUD, sync), send/receive (data-plane, simplified — no real partitioning/checkpointing) | M | done |

Mirrors Service Bus's split (Phase 6): namespace-level resources
(Event Hubs namespaces) are async (matching real Azure's LRO pattern),
child resources (Event Grid topics, event subscriptions, event hubs,
consumer groups) are sync.

Event Grid event subscriptions with a webhook `endpointType` deliver
**real** HTTP POSTs on publish, not just validate-and-discard — a
direct port of the pattern gcp-emulator already shipped for Pub/Sub
push delivery (`internal/services/pubsub`'s `deliverPush`): a
`*http.Client` with a short timeout, a fire-and-forget goroutine per
publish, the standard EventGridEvent JSON wire shape, no retry or
dead-lettering (documented limitation, same as gcp-emulator's and the
same convention already used for Phase 20's action-group webhook
dispatch). The dispatch result is recorded via two emulator-only
fields on the event subscription — `lastDeliveryStatus`/
`lastDeliveryTime` — not part of real Azure's shape but harmless,
same convention as Phase 20's `lastNotificationStatus`/
`lastNotificationTime`. Event Grid topics expose a data-plane publish
endpoint at `{topic}.eventgrid/api/events` (path-style routing,
matching the existing `.eventgrid`-suffix convention shared with
blob/queue/table/Service Bus/Cosmos DB), accepting an array of events
and fanning each one out to every event subscription on the topic. As
planned, Event Hubs partitioning/checkpointing stays out of scope —
send/receive is a simplified flat offset-ordered log per event hub,
with `lastDeliveryStatus`-equivalent observability not needed since
there's no push delivery on this side; a consumer group's GET reads
from the same underlying log as the direct `{hub}/messages` path
rather than tracking its own independent checkpoint.

Implemented in two new packages, `internal/services/eventgrid` and
`internal/services/eventhub`, both registered in `cmd/azure-emulator/
main.go` and `resourcemanager.go`'s `registeredNamespaces`
(`Microsoft.EventGrid`/`Microsoft.EventHub`). Event Grid topic/event
subscription deletes are idempotent (204 if missing, matching resource
groups' convention); Event Hubs namespace/hub/consumer-group deletes
follow the same convention, with namespace delete being async (202)
like the namespace create, matching Service Bus's and AKS's async
delete pattern. Confirmed via `eventgrid_test.go`/`eventhub_test.go`
(`httptest`, covering topic/event subscription/event hub/consumer
group CRUD, publish fan-out, and the webhook dispatch outcome fields),
the `az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1` — Event
Grid topic create → event subscription with a webhook destination →
publish → GET confirming a recorded delivery attempt; Event Hubs
namespace create (async) → event hub → consumer group → send → receive
via both the direct path and the consumer-group path → cleanup
deletes), and a real `terraform apply`/`destroy` cycle
(`terraform/smoke-test/main.tf`, via the `http` provider — 55
resources applied/destroyed including the new Event Grid/Event Hubs
blocks, with `data.http` read-backs confirming the topic's publish
endpoint, the event subscription's recorded webhook-delivery-attempt
fields, and a roundtrip send/receive on the event hub). The webhook
destination used in both smoke tests is a deliberately unreachable
placeholder URL (`localhost:10999`), so the recorded outcome is a real
connection-refused failure captured in `lastDeliveryStatus` — proving
the dispatch is a genuine outbound HTTP attempt, not a stub, the same
verification approach already used for Phase 20's action-group webhook
delivery.

## Phase 18 — API Management ✅ completed

Standalone. Lower priority than Phases 15–17 — API Management's real
value (policies, real gateway behavior, developer portal) is the part
that's hardest to emulate meaningfully, so this phase intentionally
stays narrow.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| APIM service instance (ARM CRUD, async) | resource groups | `azurerm_api_management`; the gateway/portal resource itself — provisioning in real Azure takes 30-45 minutes, a good candidate to fake as a long LRO that always succeeds | M | done |
| APIs + operations (ARM CRUD, sync, sub-resources) | APIM service | `azurerm_api_management_api`; defines the shape of what's "published", no real proxying behind it | S | done |
| Products + subscriptions (ARM CRUD, sync) | APIs | `azurerm_api_management_product`/`_subscription`; commonly provisioned alongside APIs in Terraform configs | S | done |

No request proxying, no policy evaluation, no real gateway runtime —
this phase only makes the ARM control-plane resources that
`azurerm_api_management*` Terraform resources expect to create/read/
destroy.

Implemented in a new `internal/services/apimanagement` package,
registered in `cmd/azure-emulator/main.go` and `resourcemanager.go`'s
`registeredNamespaces` (`Microsoft.ApiManagement`, `service`). The
service instance follows the same "create-async, always succeeds"
pattern as AKS's managed clusters (Phase 13): `provisioningState`
always reports `"Succeeded"`, and `gatewayUrl`/`portalUrl`/
`developerPortalUrl`/`managementApiUrl`/`scmUrl`/`publicIPAddresses`
are deterministic per-instance fake values (FNV-32a hash of the
resource's full ARM ID, same derivation pattern as Public IP's fake
address in Phase 12 and AKS's fake `fqdn` in Phase 13), so they stay
stable across repeated GETs. APIs, API operations, and products are
fully synchronous ARM sub-resources (matching App Service's/AKS's
sub-resource conventions); the product-API association is a sync PUT
with no body, mirroring the shape `azurerm_api_management_product_api`
expects. Subscriptions are synchronous and get deterministic fake
`primaryKey`/`secondaryKey` values (same FNV-32a-seeded `fakeGUID`
helper, seeded from the subscription's resource ID), confirmed
reproducible across independent runs and independent tools (identical
keys observed from both the PowerShell and bash smoke-test runs
against fresh DBs). Deleting the service instance cascades
synchronously to all of its APIs, products, and subscriptions before
returning the async delete response, matching AKS's "deleting a
cluster deletes its node pools" cascade convention from Phase 13.
Service/API/product/subscription deletes are idempotent (204 if
missing, matching resource groups' convention). Confirmed via
`apimanagement_test.go` (`httptest`, covering service lifecycle,
publisher-field validation, API/operation lifecycle and cascade-on-
parent-delete, missing-parent-service 404, product/subscription
lifecycle, and full-service-delete cascading over all sub-resources),
the `az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1` — service
PUT/GET/LIST → API PUT/GET → operation PUT/GET → product PUT →
product-API association → subscription PUT/GET (deterministic
primaryKey/secondaryKey) → cleanup deletes in reverse dependency
order), and a real `terraform apply`/`destroy` cycle
(`terraform/smoke-test/main.tf`, via the `http` provider — 61 resources
applied including the new API Management block, with `data.http`
read-backs confirming the service instance's deterministic
`gatewayUrl`, the product's id, and the subscription's `scope`
matching the product's resource ID).

## Phase 19 — ARM template / Bicep deployments ✅ completed

Cross-cutting rather than a single resource family: this phase makes
`Microsoft.Resources/deployments` itself work, so a whole template
(JSON ARM template or compiled-from-Bicep JSON) can be submitted in
one call instead of one resource per `az rest`/Terraform resource
block.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| `PUT /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Resources/deployments/{name}` (async) | every resource type the submitted template references | `az deployment group create`; needs to parse a `resources[]` array and dispatch each entry to the matching existing service's internal handler rather than re-implementing resource creation | L | done |
| `GET .../deployments/{name}` + `deployments/{name}/operations` (sync) | the deployment PUT above | Terraform/az CLI poll these to know when a deployment LRO finished and to show per-resource operation results | S | done |
| `POST .../deployments/{name}/validate` (sync, always succeeds) | — | `az deployment group validate`/Terraform's plan-adjacent dry-run; the emulator has no real validation rules to enforce, so this is a shape-only stub | S | done |

Implemented in a new `internal/services/deployments` package,
registered in `cmd/azure-emulator/main.go` and `resourcemanager.go`'s
`registeredNamespaces` (`Microsoft.Resources`, `deployments`). The
dispatcher walks the submitted template's `resources[]` array,
resolves a deliberately incomplete subset of ARM template expressions
(`parameters('x')`, `variables('x')`, `resourceId(type, name...)`),
topologically orders entries by `dependsOn` (detecting cycles as a
400), and forwards each resolved resource as a synthetic in-process
HTTP PUT to the matching existing service's own handler — not a
reimplementation of resource creation. The deployment PUT itself is
async (LRO), matching `az deployment group create`'s real behavior;
`GET .../deployments/{name}` and `.../operations` are synchronous
reads, and `POST .../validate` resolves/dispatches through the same
expression-and-ordering logic but always against a dry-run path that
never persists anything, so it can confirm template shape without
side effects. A failed dispatched resource (e.g. a missing required
field) marks the whole deployment `Failed` with a populated `error`
and leaves that resource uncreated, mirroring real Azure's
all-the-way-through failure propagation; deleting a deployment removes
only its own record, never the resources it created — same
purposely-narrow scope as `what-if`, which stays out of scope entirely
(it would require diffing against current state in a way none of the
other phases need). Confirmed via `deployments_test.go` (`httptest`,
covering dispatch + operation persistence, `dependsOn` topological
ordering, dependency-cycle detection, missing-required-parameter
validation, dispatched-resource failure propagation, `validate` not
creating resources, and delete-is-idempotent-but-doesn't-cascade), the
`az rest` smoke tests (`scripts/test-az-cli.sh`/`.ps1` — PUT a
deployment templating a real storage account via
`parameters()`/`variables()`/`resourceId()` → GET deployment
(`provisioningState: Succeeded`) → list operations (one Succeeded
entry) → GET the dispatched storage account (proving it was really
created) → POST validate (shape-only, creates nothing) → DELETE
deployment → GET the storage account again (must survive) → cleanup),
and a real `terraform apply`/`destroy` cycle against both Terraform
configs: `terraform/smoke-test/main.tf` (via the `http` provider, a
`null_resource`+`local-exec` PUT plus `data.http` read-backs of the
deployment and its dispatched storage account) and
`terraform/azurerm-smoke-test/main.tf` (via the real `azurerm`
provider's `azurerm_resource_group_template_deployment` resource,
using the same `parameters()`/`resourceId()` template that
`TestDeploymentDispatchesResourceAndPersistsOperations` exercises).

## Phase 20 — Action Groups: real webhook delivery ✅ completed

Standalone upgrade to the already-implemented Phase 10 `monitor`
package — no new package, no new ARM resource. Smallest and
lowest-risk step in the behavioral-layer plan, since the shape
(`ActionGroupProperties.WebhookReceivers`) already existed and was
already persisted; only the dispatch was missing.

| Component | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| `dispatch()` real HTTP POST to each `webhookReceivers` entry | Action groups (Phase 10) | Closes the gap the old comment in `actiongroups.go` called out explicitly ("no se envía ninguna notificación real") for the webhook receiver type only — email/SMS/Azure Function receivers stay shape-only, same as before | S | done |
| `POST .../actionGroups/{name}/createNotifications` (sync action) | `dispatch()` above | Real Azure Monitor API's own test-notification action name (revised from the originally-planned `:fireTestNotification` — Azure uses a literal sub-path, same convention as `start`/`stop`/`restart` in `appservice`, not a gcp-style colon action) | S | done |

Implemented in `internal/services/monitor/actiongroups.go`/`monitor.go`,
directly modeled on gcp-emulator's Cloud Tasks/Cloud Scheduler dispatch
pattern (`internal/services/cloudtasks`, `internal/services/cloudscheduler`):
a `*http.Client{Timeout: 10 * time.Second}` held on `Service`, a real
`http.NewRequestWithContext` POST per `webhookReceivers` entry, no
retry, no dead-lettering — explicit documented limitation, same
convention used everywhere else in this project (Key Vault's simulated
crypto, AKS's unevaluated `provisioningState`). Unlike Cloud
Scheduler's long-running goroutine, `dispatch()` runs synchronously
inside the `createNotifications` request, since the only trigger is
that one explicit action, not a continuous metrics pipeline (metric
alert `criteria` is still never evaluated, same as Phase 10). The
outbound body is a minimal subset of Azure Monitor's "common alert
schema" (`schemaId: azureMonitorCommonAlertSchema`,
`data.essentials.{alertId, alertRule, severity, signalType,
monitorCondition, firedDateTime}`) — enough for a real webhook
receiver to recognize a test dispatch, not a full replica. This
emulator doesn't have gcp-emulator's `internal/activity` (a shared
Logging+Monitoring event sink), so the result is recorded via two
emulator-only fields added to `ActionGroupProperties` —
`lastNotificationTime`/`lastNotificationStatus` — not part of real
Azure's shape, but harmless since real clients (az CLI/Terraform)
ignore unknown JSON response fields; a real activity-log sink remains
deferred to Phase 22 or later. Confirmed via `monitor_test.go`
(`httptest`, covering a successful dispatch with payload assertion and
a 404 on a missing action group), and a live manual end-to-end
verification: the real emulator binary running against a real
independent HTTP listener on a different port, driven through `az
rest` (`PUT` resource group → `PUT` action group with a
`webhookReceivers` entry → `POST createNotifications` → `GET` action
group), confirming the listener actually received the common-alert-
schema JSON payload over the network and that
`lastNotificationStatus`/`lastNotificationTime` were persisted.
Terraform coverage is intentionally unchanged: `azurerm_monitor_action_group`
already exercises the `webhook_receiver` shape (Phase 10), but
`createNotifications` is an imperative test action with no declarative
Terraform resource/data-source mapping to it — only `az rest` (or the
real `az monitor action-group test-notifications create` CLI command)
can invoke it, so this phase is covered by az CLI smoke testing only,
not Terraform.

## Phase 21 — Scheduler-equivalent: Logic App Recurrence trigger ✅ completed

Net-new service — unlike Phase 20, there's no existing azure-emulator
resource to extend, and unlike Event Grid (Phase 17), there's no
already-planned shape-only phase to upgrade. This is the Azure
analog of gcp-emulator's Cloud Scheduler, and is scoped narrowly on
purpose: real Logic Apps have dozens of trigger/action types and a
full designer-driven workflow language, none of which is in scope
here.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| New `internal/cronlike` package: minimal recurrence evaluator (interval + frequency: Second/Minute/Hour/Day/Week/Month, optional `startTime`) | — | Reimplementation (not a copy) of gcp-emulator's `internal/cronexpr`, adapted to Logic Apps' `recurrence` object shape (`{"frequency": "Hour", "interval": 1}`) instead of 5-field unix-cron, since that's the shape `azurerm_logic_app_workflow`/ARM actually send | M | done |
| `Microsoft.Logic/workflows` (ARM CRUD, sync) — Consumption plan only, `definition.triggers` restricted to a single `Recurrence` trigger, `definition.actions` restricted to a single `Http` action | recurrence evaluator above | `azurerm_logic_app_workflow`; the minimum shape that lets a workflow be provisioned and have something real happen | M | done |
| Per-workflow firing goroutine, resume-on-restart from BoltDB state | workflows above | Same pattern as gcp-emulator's Cloud Scheduler `startFiring`/`stopFiring`/`fireLoop`: a `map[string]chan struct{}` of stop signals, relaunched in `New()` for every workflow whose state is enabled | M | done |
| `POST .../workflows/{name}/triggers/{trigger}/run` (manual trigger action) | workflows above | Mirrors Cloud Scheduler's `:run` — lets the az CLI/Terraform smoke tests fire a workflow on demand instead of waiting for the schedule | S | done |

Any `actions` entry beyond a single `Http` action stays unparsed
passthrough JSON, same "shape-compatible, not behavior-complete"
convention used for AKS/Key Vault — this phase is not a workflow
interpreter, just enough to prove a recurrence trigger really fires an
HTTP call on schedule. `enabled`/disabled workflows reuse the
pause/resume semantics already established by AKS agent pool actions
and App Service's start/stop/restart. Considered and rejected:
piggybacking on Service Bus or Event Grid instead of a new package —
neither's real Azure API exposes a cron-like recurrence concept, so
forcing one in would misrepresent the real service's shape, which
this project has consistently avoided (see the no-referential-integrity
convention notes throughout Phases 10-15).

Implemented in two new packages: `internal/cronlike` (`Recurrence{Frequency,
Interval, StartTime}`, `Validate()`, and `Next(recurrence, created, after)`
computing the next fire time for `Second`/`Minute`/`Hour`/`Day`/`Week`/`Month`
frequencies) and `internal/services/logicapps` (`Microsoft.Logic/workflows`
ARM CRUD, registered in `cmd/azure-emulator/main.go` and
`resourcemanager.go`'s `registeredNamespaces`). Workflows are fully
synchronous (no LRO), matching App Service's/AKS's-sibling sync resource
families rather than AKS's own async pattern, since there's no
multi-minute provisioning step to fake here. `definition.triggers` is
restricted to exactly one `Recurrence`-type trigger and `definition.actions`
to exactly one `Http`-type action — anything else in either map is rejected
with 400, rather than silently ignored, so a misconfigured workflow fails
fast instead of looking like it's running. On every successful PUT, the
service (re)launches a per-workflow goroutine (`startFiring`) that sleeps
until `cronlike.Next` says the recurrence is due, performs a real
`http.NewRequestWithContext` POST to the `Http` action's `uri` (same
`*http.Client{Timeout: ...}` fire-and-forget dispatch pattern as Event Grid's
webhook delivery in Phase 17 and Action Groups' in Phase 20, no retry, no
dead-lettering), and loops — stopped via a `map[string]chan struct{}` of
stop signals on delete or on `state` transitioning away from `"Enabled"`,
and relaunched for every persisted workflow whose `properties.state` is
still `"Enabled"` when `New(db)` runs, so a server restart resumes firing
without the caller having to re-PUT anything (same resume-on-restart
convention as gcp-emulator's Cloud Scheduler). `POST
.../triggers/{trigger}/run` is deliberately **synchronous** — unlike the
automatic recurrence firing, it blocks until the one-off dispatched HTTP
call returns (or times out) and reports the outcome immediately via
`lastRunStatus`/`lastRunTime` on the workflow, so a caller (or a smoke test)
gets a deterministic, immediately-checkable result instead of having to
poll. Workflow deletes are idempotent (204 if missing, matching resource
groups' convention) and stop the firing goroutine first. Confirmed via
`cronlike_test.go` and `logicapps_test.go` (`httptest`, covering
`Next`'s frequency/interval/startTime math, trigger/action shape
validation, the manual-run endpoint's synchronous outcome reporting, and
delete stopping the firing goroutine), and — going beyond the Go test
suite, as this phase's behavior is specifically about something really
happening on a timer — a live, real end-to-end run against the actual
emulator binary for all three smoke-test surfaces: `scripts/test-az-cli.ps1`,
`scripts/test-az-cli.sh`, and `terraform/smoke-test/main.tf`. Each one
starts a real, dedicated counter-listener process (`scripts/
webhook-counter-listener.ps1`/`.py` — a minimal HTTP server that counts
every POST it receives except to its own `/count` path, exposing the
count via `GET /count`) rather than reusing Phase 20's never-started
`localhost:10999/webhook` placeholder, because this phase's manual-run
and automatic-recurrence requirements need a *positive* confirmation of
real receipt, not just a recorded delivery-attempt status field. Each
surface: PUTs a workflow with a `Recurrence` trigger (`interval: 5`
seconds) and an `Http` action pointed at the listener, GETs it back,
POSTs a manual trigger run and confirms the listener's counter is at
least 1 immediately after, sleeps 7 seconds (more than one recurrence
interval) and confirms the counter has gone *up further* (proving an
automatic, unprompted fire actually happened on schedule, not just the
manual one), then deletes the workflow and stops the listener. In
Terraform, the "count increased" assertions are enforced by a
`null_resource`/`local-exec` PowerShell script that `throw`s (failing
`terraform apply` with a non-zero exit) if either condition isn't met,
since the generic `http` provider's `data` sources have no native
assertion mechanism. All three runs passed live against a real running
instance of the emulator: the manual run produced exactly 1 received
POST, and after the 7-second wait the counter had reached 2, confirming
a genuine unprompted recurrence fire.

## Phase 23 — Azure SQL Database ✅ completed

Net-new service, picked from the "Phase 23+ candidate services"
breadth list below as the first of the two highest-value, most
commonly Terraformed candidates (alongside Container Registry). No
real query engine: databases are ARM records carrying fake properties
(`collation`, `maxSizeBytes`, `sku`), in keeping with the project's
established "shape-compatible, no behavior-complete" convention for
data planes that don't need real behavior to satisfy az CLI/Terraform.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| `Microsoft.Sql/servers` (ARM CRUD, sync) | — | `azurerm_mssql_server`; `administratorLoginPassword` accepted but never persisted/returned, same convention as `compute.OsProfile.AdminPassword` | S | done |
| `databases` sub-resource (ARM CRUD + PATCH, sync) | servers above | `azurerm_mssql_database`; single-level nested sub-resource, same pattern as `eventhub.putHub` | M | done |
| `firewallRules` sub-resource (ARM CRUD, sync) | servers above | `azurerm_mssql_firewall_rule`/`azurerm_sql_firewall_rule` | S | done |
| `connectionPolicies` singleton (PUT/GET) + collection GET | servers above | `azurerm_mssql_server` PUTs this unconditionally after creating a server, even without an explicit `connection_policy` attribute; the provider's LRO poller also GETs the bare collection (not the singleton) to decide "are we done yet?" | S | done |
| `restorableDroppedDatabases`, `backupLongTermRetentionPolicies`, `backupShortTermRetentionPolicies`, `securityAlertPolicies`, `transparentDataEncryption` (all GET-only) | databases above | `azurerm_mssql_server`/`azurerm_mssql_database`'s `Read` queries these unconditionally on every refresh/plan; without a response the provider treats the resource as broken, not just "feature unused" | S | done |

Implemented in `internal/services/sql` (`servers.go`, `databases.go`,
`firewallrules.go`, `connectionpolicy.go`, `databasepolicies.go`),
registered in `cmd/azure-emulator/main.go` and `resourcemanager.go`'s
`registeredNamespaces` under `Microsoft.Sql`. Servers and their two
direct sub-resources (databases, firewallRules) are fully synchronous —
no LRO — matching Key Vault's/Managed Identity's sync resource family
rather than AKS's async one, since provisioning a logical SQL server
in real Azure has no equivalent multi-minute step worth faking here.
`databases` additionally registers a `PATCH` handler routed to the same
`putDatabase` function as `PUT`, since the real provider uses PATCH
(not PUT) for in-place updates such as changing `sku_name`/
`storage_account_type` without recreating the database; `putDatabase`
was already idempotent and preserves `creationDate` when the database
existed before, so no new logic was needed beyond registering the
extra method. `connectionPolicies` needed both a singleton route
(`.../connectionPolicies/{policyName}`, normally `default`) **and** a
bare-collection `GET .../connectionPolicies` route — discovered only
through live Terraform testing, not from reading the provider's
attribute schema — because `azurerm_mssql_server`'s LRO poller checks
the collection endpoint, not the singleton, to decide whether its PUT
finished; without the collection route the poller's GET 404s and the
provider reports the whole operation as failed even though the
underlying PUT had actually already succeeded.

A field-mapping bug surfaced only by a full
`destroy`/`apply`/`plan -detailed-exitcode` cycle against the real
`azurerm` provider (not caught by any Go unit test, since the bug was
in what JSON field name the *real* provider's Go source reads, not in
this project's own code shape): `azurerm_mssql_database`'s `Read`
populates the Terraform attribute `sku_name` from
`properties.currentServiceObjectiveName` — **not** the top-level
`sku.name` field one would reasonably assume — and populates
`storage_account_type` from
`properties.requestedBackupStorageRedundancy` — **not**
`properties.currentBackupStorageRedundancy`. Confirmed directly against
the `terraform-provider-azurerm` source
(`mssql_database_resource.go`: `skuName = *props.CurrentServiceObjectiveName`,
`BackupStorageRedundancy = string(*props.RequestedBackupStorageRedundancy)`).
Without the correctly-named fields, every `terraform plan` showed a
false `+ sku_name`/`+ storage_account_type` diff even immediately after
a clean apply. Fixed by adding `CurrentServiceObjectiveName` (derived
from the same `sku` value, so there's a single source of truth) and
`RequestedBackupStorageRedundancy` to `DatabaseProperties`, alongside
the original `CurrentBackupStorageRedundancy` field (kept in case a
different provider version reads that one instead). Confirmed via
`sql_test.go` (`httptest`, covering server/database/firewall-rule CRUD,
the database PATCH path, and the policy/connection-policy sub-resource
defaults), the az CLI smoke tests (`scripts/test-az-cli.sh`/`.ps1` —
create server/database/firewall rule, list, get, delete), and a real
Terraform `azurerm` provider apply/destroy cycle
(`terraform/azurerm-smoke-test/`): a from-scratch `destroy` →
`apply` → `plan -detailed-exitcode` run produced exit code 0 ("No
changes. Your infrastructure matches the configuration.") with zero
diff, the first time this kind of provider-Read field-mapping issue has
been driven all the way to a definitively confirmed fix in this project.

## Phase 24 — Azure Container Registry ✅ completed

Net-new service, the second of the two phases picked from the
candidate breadth list alongside SQL Database — ACR is one of the most
commonly Terraformed Azure resources and, unlike AKS/Service Bus/
Cosmos DB, needed no existing package to extend. No real image
registry behind it (no push/pull, no manifest storage): `loginServer`
is a deterministic fake hostname, "shape-compatible, no
behavior-complete" like the rest of this project's simplified data
planes.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| `Microsoft.ContainerRegistry/registries` (ARM CRUD, sync) | — | `azurerm_container_registry`; `loginServer` deterministically derived as `{name}.azurecr.io` (lowercase), same global-uniqueness assumption as real ACR | S | done |
| `checkNameAvailability` (subscription-scoped action) | registries above | `azurerm_container_registry` calls this before every PUT; ACR names live in a global namespace (unlike resource-group-scoped resources), so this check is subscription-scoped, not resource-group-scoped | S | done |
| `listCredentials` action | registries above | `azurerm_container_registry`'s `Read` calls this whenever `admin_enabled=true`, to populate `admin_username`/`admin_password` | S | done |
| `replications` collection (GET-only) | registries above | `azurerm_container_registry`'s `Read` always queries this collection (to reconcile the `georeplications` attribute), even when geo-replication was never configured | S | done |

Implemented in `internal/services/containerregistry`
(`containerregistry.go`, `registries.go`), registered in
`cmd/azure-emulator/main.go` and `resourcemanager.go`'s
`registeredNamespaces` under `Microsoft.ContainerRegistry`. `registries`
is fully synchronous, matching Key Vault's/SQL servers' sync resource
family. `RegistryProperties` always populates `networkRuleSet`/
`policies`/`encryption` sub-objects with "off"/empty values, even
though the emulator enforces none of them and the chosen SKU might not
support them in real Azure (e.g. Basic doesn't support network rules) —
this was a deliberate design choice, not an oversight: the real
provider's flatten functions (`flattenNetworkRuleSet`,
`resourceContainerRegistryRead`) dereference these sub-objects and
range over their inner slices without a nil check, so omitting them
from the JSON causes the *provider itself* to panic with a nil-pointer
dereference instead of failing cleanly — `ipRules`/`virtualNetworkRules`
specifically must be present as empty arrays (`[]`), not omitted,
for the same reason. `checkNameAvailability` doesn't simulate a real
global ACR namespace (there isn't one behind this emulator) — it
reports a collision only against registries that already exist in the
same subscription, which is enough to satisfy the provider's
pre-flight check without over-claiming real cross-tenant uniqueness
enforcement. Confirmed via `containerregistry_test.go` (`httptest`,
covering registry CRUD, `checkNameAvailability`'s collision detection,
`listCredentials`'s fake credential generation, and `replications`'
always-empty response), the az CLI smoke tests (`scripts/
test-az-cli.sh`/`.ps1` — create registry, check name availability, list
credentials, list, get, delete), and a real Terraform `azurerm` provider
apply/destroy cycle (`terraform/azurerm-smoke-test/`) as part of the
same from-scratch `destroy`/`apply`/`plan -detailed-exitcode` run
described under Phase 23, which also exercised `acr_login_server` as a
Terraform output and ended in a confirmed zero-diff `plan`.

## Phase 22 — Pluggable real-execution backends 💭 proposed (not yet planned)

Large, optional, and explicitly lower-confidence than Phases 16-21 —
this mirrors gcp-emulator's own "Phase 12" status (proposed, not
committed) for the same reason: it's the first phase in this project
that would make Docker a *meaningful* (if still optional) part of the
runtime story, which is a bigger architectural commitment than
anything shipped so far.

Proposed shape, subject to revision before any implementation starts:

| Component | Why | Effort | Status |
|---|---|---|---|
| Per-resource `backend=real` opt-in (query param or a tag/label convention) | Keeps the default zero-dependency behavior; nothing breaks for anyone not opting in, same philosophy as `-tls` being optional | M | proposed |
| Docker-engine detection at startup, automatic fallback to shape-only if absent | Never makes Docker mandatory, never fails silently — matches gcp-emulator's stated design goal for its own equivalent feature | S | proposed |
| Real backend candidate 1: App Service/Function Apps → `docker run` the user's container image, fronted by a reverse proxy | The most natural fit — `azurerm_linux_web_app`'s `application_stack.docker_image_name` already names a real image; today it's just persisted and never run | L | proposed |
| Real backend candidate 2: a real embedded Postgres for an eventual Azure Database for PostgreSQL Flexible Server resource (not yet on this roadmap at all) | Mirrors gcp-emulator's committed Cloud SQL/Postgres scope; would need that ARM resource type added first since this project has no Postgres-shaped service yet | L | proposed |
| Resource governor (idle backend eviction, `EMULATOR_MAX_REAL_BACKENDS` override) | Without this, a long-running emulator with several `backend=real` resources could exhaust the host's RAM | M | proposed |

This phase is intentionally written as a proposal, not a committed
plan — it should be revisited (and likely re-scoped down, the same way
gcp-emulator's own committed real-execution scope ended up smaller
than its Phase 12 brainstorm) once Phases 16-21 are done and there's a
clearer sense of whether real-execution depth is actually the
highest-value next investment versus more resource-type breadth.

## Phase 25+ — candidate services to broaden coverage 💭 proposed (not yet planned)

Phases 0-21 and 23-24 are all done; Phase 22 is an explicitly
speculative architecture change (real backends), not a breadth play.
The list below is the other direction: more Azure resource types, same
shape-only/sync-CRUD-plus-the-occasional-real-behavior philosophy as
everything shipped so far, picked by cross-referencing what's already
implemented against what `az`/`azurerm` workflows commonly touch that
this emulator can't fake yet. Azure SQL Database and Azure Container
Registry — formerly the top two entries here — are now implemented as
Phase 23 and Phase 24 respectively (see above). Nothing below is
committed or scheduled — this is a menu to pick from, not a queue.

| Candidate | Why it'd be valuable | Effort | Status |
|---|---|---|---|
| Azure Database for PostgreSQL/MySQL Flexible Server (`Microsoft.DBforPostgreSQL`/`Microsoft.DBforMySQL`) | Second-most-common managed-DB resource after SQL; also the natural Phase 22 "real backend candidate 2" precursor since it doesn't exist as a shape yet | M | proposed |
| Azure Container Instances (`Microsoft.ContainerInstance/containerGroups`) | Common quick-deploy target in `azurerm` examples; sync ARM CRUD with a fake IP/FQDN, same depth as AKS's "shape-compatible, not behavior-complete" stance | S | proposed |
| Redis Cache (`Microsoft.Cache/redis`) | Extremely common companion resource alongside App Service/Functions in real configs; ARM CRUD plus fake `primaryKey`/`hostName`, no real cache behind it | S | proposed |
| Recovery Services vault (`Microsoft.RecoveryServices/vaults`) + backup policy shapes | Shows up in compliance-oriented Terraform configs; pure shape (no real backup execution), low effort since it's mostly a container resource | S | proposed |
| Azure Policy (`Microsoft.Authorization/policyDefinitions`, `/policyAssignments`) | Natural extension of the existing `internal/services/authorization` package (Phase 15) rather than a new package; no real policy evaluation, just CRUD + assignment scoping | S | proposed |
| Storage file shares (extend `internal/services/storageaccounts`) | Storage accounts already exist; file shares are a small data-plane addition reusing the existing account/auth plumbing rather than a new service | S | proposed |
| Static Web Apps (`Microsoft.Web/staticSites`) | Small extension of the existing App Service package (Phase 11); ARM CRUD plus a fake default hostname | S | proposed |
| Notification Hubs / SignalR Service | Rounds out the "Eventing" family from Phase 17 with two more commonly-paired-with-Functions resources; ARM CRUD only | S | proposed |
| Private Endpoints / Private Link (extend `internal/services/network`) | The most common real-Terraform networking gap versus what Phase 12 already covers; shape-only (no real network isolation, same as everything else in `network`) | M | proposed |

Suggested ordering if/when this gets picked up: Redis Cache and
Container Instances first (small, same day, highest real-world
frequency among what's left), then the rest opportunistically. Each
would follow the same per-phase checklist as every prior phase:
implement, unit test, extend the `az rest` + Terraform smoke tests,
update README/ROADMAP, scoped commit with a push command handed to the
user.

## Maintenance / cross-cutting (no fixed phase number)

These aren't blocked on anything above and can be picked up whenever
useful, independent of the phase order:

- ✅ **`scripts/test-az-cli.sh`/`.ps1` state cleanup**: fixed by adding a
  delete-then-create idempotent setup step (errors ignored) right
  before the table/entity creation calls, so a re-run after a
  partial/interrupted prior run no longer fails with
  `TableAlreadyExists`/`EntityAlreadyExists` `Conflict`. Verified live
  with two consecutive full runs of `test-az-cli.sh` against the same
  emulator instance, both completing cleanly.
- ✅ **Web console catch-up**: `web/console` (Phase 7) originally only
  covered the six resource types implemented through Phase 6. Added
  Monitor/Log Analytics (Phase 10: workspaces, action groups, metric
  alerts), App Service (Phase 11: plans, sites), the Phase 12
  networking additions (virtual networks, NSGs, public IPs, load
  balancers, route tables, private DNS zones), AKS (Phase 13: managed
  clusters), and Functions (Phase 14: function definitions under a
  Function App). Each new section follows the existing
  load/create/delete pattern; sub-resources not yet exposed in the UI
  (subnets, security rules, route entries, DNS record sets, agent
  pools, app settings) stay on `az`/Terraform for now. Verified the
  exact request/response shapes against the running emulator via curl
  before wiring up the JS.
- ✅ **Real `azurerm` provider deployment-lifecycle bugs**: a live
  `terraform init`/`plan`/`apply`/`destroy` PoC against the real
  `hashicorp/azurerm` provider (not the generic `http` provider) surfaced
  three bugs in the ARM template-deployment lifecycle that no existing
  unit test or `az`/`http`-provider smoke test exercised: a missing
  `POST .../deployments/{name}/exportTemplate` endpoint (added, backed by
  a new `deployments.templates` BoltDB bucket), a missing
  `properties.providers` field on `DeploymentProperties` needed for
  destroy-time cleanup (added, populated by a new `buildProviders()`
  helper in `internal/services/deployments`), and missing `apiVersions`
  on `resourcemanager.ProviderResourceType` needed by the provider to
  pick a version when deleting each resource type (added across every
  entry in `registeredNamespaces`, not just `Microsoft.Storage`, to avoid
  the same failure mode for other namespaces). Verified via unit tests
  plus a full clean live re-run of the four-step lifecycle, captured in
  [`docs/poc-terraform-azurerm.md`](docs/poc-terraform-azurerm.md).
- ✅ **`internal/server/armcase.go` double-slash normalization**: a live
  Phase 23/24 Terraform PoC surfaced that the real `azurerm` provider
  (via `go-azure-sdk`), when built against a custom `metadata_host`,
  sometimes concatenates a base URL already ending in `/` with a
  resource ID that also starts with `/`, producing requests like
  `https://host//subscriptions/...`. `net/http.ServeMux` normally
  cleans that up itself, but does so via a 307 redirect — and
  `go-azure-sdk` doesn't follow redirects on write requests (PUT/POST/
  DELETE), so those calls failed outright. Fixed with a new
  `collapseSlashes` helper, applied before ARM case normalization in
  `withARMCaseNormalization`, that collapses any run of repeated `/`
  into one. Verified as part of the same from-scratch
  `destroy`/`apply`/`plan -detailed-exitcode` Terraform run described
  under Phase 23/24, which completed with zero errors and a confirmed
  zero-diff plan.

Further phases beyond these will keep being added as unplanned phases
once the above is solid, the same way gcp-emulator grew past its
original 8 phases.
