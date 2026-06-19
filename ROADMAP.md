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
alongside the existing VNet/subnet/NIC resources. See the table below
for the per-phase breakdown.

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

Future phases (AKS, Functions, ARM custom roles/RBAC) will be added as
unplanned phases once the above is solid, the same way gcp-emulator
grew past its original 8 phases.
