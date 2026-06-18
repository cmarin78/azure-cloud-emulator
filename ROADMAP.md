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
image. Phase 3 (Storage) is underway: storage account ARM CRUD is
done; blob/queue/table data-plane is next — see the table below.
Each service is now also covered by an `az rest` smoke test
(`scripts/test-az-cli.sh`/`.ps1`) and a Terraform smoke test
(`terraform/smoke-test/`, using the `http` provider since `azurerm`
needs ARM metadata discovery + real AAD this emulator doesn't
implement yet).

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
| Blob containers/blobs (data-plane: create/list/get/delete, upload/download) | storage accounts | Most common Terraform/SDK usage (`azurerm_storage_container`, `azurerm_storage_blob`) | M | — |
| Queue storage (CRUD, enqueue/dequeue) | storage accounts | `azurerm_storage_queue` | S | — |
| Table storage (CRUD, basic entity operations) | storage accounts | `azurerm_storage_table` | S | — |

## Phase 4 — Compute

Depends on Resource Manager (Phase 2) for resource groups, and benefits
from the LRO helper (Phase 1) since every Compute mutation in real Azure
is asynchronous.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Virtual networks + subnets | resource groups | `azurerm_virtual_network`/`azurerm_subnet`; required by NICs | S | — |
| Network interfaces (NICs) | virtual networks | `azurerm_network_interface`; required by VMs | S | — |
| Managed disks | resource groups | `azurerm_managed_disk`; required by VM OS disk | S | — |
| VM images (static catalog, e.g. Ubuntu 22.04, Windows Server 2022) | — | Required by VM image reference | S | — |
| Virtual machines (create/list/get/delete, start/stop) | NICs, disks, images | `azurerm_linux_virtual_machine`/`azurerm_windows_virtual_machine` | L | — |

Target: `terraform apply`/`destroy` against `azurerm_virtual_network` +
`azurerm_network_interface` + `azurerm_linux_virtual_machine` works
without provider patches, matching the bar gcp-emulator hit for
`google_compute_instance`.

## Phase 5 — Key Vault

Standalone, no dependency on Compute/Storage beyond a resource group.

| Resource | Depends on | Why | Effort | Status |
|---|---|---|---|---|
| Vaults (ARM-level CRUD) | resource groups | `azurerm_key_vault` | S | — |
| Secrets (CRUD) | vaults | `azurerm_key_vault_secret`; most common use case | S | — |
| Keys (CRUD, basic ops) | vaults | `azurerm_key_vault_key` | M | — |
| Certificates (CRUD, basic ops) | vaults | `azurerm_key_vault_certificate` | M | — |

## Phase 6 — Messaging and data

Independent of each other; each is a new package similar in size to
Storage.

| Service | Minimum resources | Effort | Status |
|---|---|---|---|
| Service Bus | namespaces, queues, topics/subscriptions, send/receive | M | — |
| Cosmos DB (SQL API) | accounts, databases, containers, document CRUD | L | — |

## Phase 7 — Web console

| Component | Note | Effort | Status |
|---|---|