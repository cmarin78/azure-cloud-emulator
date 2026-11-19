# Changelog

All notable changes to the Azure Cloud Emulator are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

---

## [0.34.0] — Phase 34: Azure App Configuration

### Added
- **`Microsoft.AppConfiguration/configurationStores`** — ARM CRUD (sync) with deterministic endpoint (`https://{name}.azconfig.io`), SKU defaults, and purge protection / retention properties.
- **`listKeys` action** — Returns deterministic primary/secondary and read-only API access keys with valid connection strings.
- **`keyValues` sub-resource** — Full ARM CRUD (`PUT`/`GET`/`LIST`/`DELETE`) under configuration stores supporting key and label scoping (`key$label`).
- **Simulated Data-Plane** — Path-style data plane endpoint (`/{store}.appconfig/kv/{key}`) with label query parameter support.
- **Web Console view** — App Configuration view in `web/console/` for browsing, creating, and deleting stores.

### Fixed
- **ARM Case Normalization (`internal/server/armcase.go`)** — Added canonical segments for all provider namespaces and resource types across Phases 10-34, preventing 404s when `azurerm` or `go-azure-sdk` send lowercased URL paths.
- **Provider Registration (`internal/services/resourcemanager`)** — Merged duplicate `Microsoft.Network` declarations in `registeredNamespaces` and registered `Microsoft.AppConfiguration`.
- **Documentation in test scripts** — Corrected obsolete claims in `test-az-cli.sh` and `test-az-cli.ps1` regarding ARM metadata and AAD token support.

### Added
- **Core Infrastructure Unit Tests** — Added comprehensive unit test suites for `internal/server/` (93.9% coverage), `internal/storage/` (86.5% coverage), and `internal/devtls/` (74.3% coverage).
- **CI Enhancements** — Added `go mod verify`, `go mod tidy` diff checking, and test coverage profile generation (`-coverprofile=coverage.out`) in `.github/workflows/ci.yml`.

---

## [0.33.0] — Phase 33: Private Endpoints + Private Link Services

### Added
- **`Microsoft.Network/privateEndpoints`** — ARM CRUD (sync), auto-approving `privateLinkServiceConnections[]` with deterministic network interface injection into `networkInterfaces[]`.
- **`privateDnsZoneGroups` sub-resource** — Full CRUD (`PUT`/`GET`/`LIST`/`DELETE`) under private endpoints.
- **`Microsoft.Network/privateLinkServices`** — ARM CRUD (sync) with deterministic alias generation and `privateEndpointConnections` stubs.

---

## [0.32.0] — Phase 32: Notification Hubs + SignalR Service

### Added
- **`Microsoft.NotificationHubs`** — Namespaces and notification hubs ARM CRUD (sync), authorization rules, and `listKeys` with deterministic `sb://` connection strings.
- **`Microsoft.SignalRService/signalR`** — ARM CRUD (sync), `serviceMode` extraction, deterministic hostname (`{name}.service.signalr.net`), and `listKeys` action.

---

## [0.31.0] — Phase 31: Recovery Services Vault

### Added
- **`Microsoft.RecoveryServices/vaults`** — ARM CRUD (sync) with Standard/RS0 SKU and publicNetworkAccess defaults.
- Read-only sub-resources: `backupConfig/vaultconfig` (soft-delete enabled, geo-redundant storage, accepting PUT as no-op), `backupPolicies` stub, and `backupStorageConfig`.

---

## [0.30.0] — Phase 30: PostgreSQL + MySQL Flexible Server

### Added
- **`Microsoft.DBforPostgreSQL/flexibleServers` & `Microsoft.DBforMySQL/flexibleServers`** — ARM CRUD (sync) with SKU, storage, backup, and high-availability configuration.
- Sub-resources: `databases` (ARM CRUD), `firewallRules` (ARM CRUD), and `configurations` stub.
- Deterministic FQDNs (`{name}.postgres.database.azure.com` / `{name}.mysql.database.azure.com`).

---

## [0.29.0] — Phase 29: Static Web Apps

### Added
- **`Microsoft.Web/staticSites`** — ARM CRUD (sync) in `internal/services/appservice`.
- `POST .../listSecrets` returning deterministic fake `apiKey` matching `azurerm_static_web_app` Read requirements.
- Deterministic hostname `{name}.azurestaticapps.net`.

---

## [0.28.0] — Phase 28: Storage File Shares

### Added
- **`fileServices` & `shares`** — Extended `internal/services/storageaccounts` with `fileServices/default` singleton and `shares/{name}` ARM CRUD.
- Configurable `shareQuota`, `enabledProtocols` (SMB/NFS), `accessTier`, and metadata.

---

## [0.27.0] — Phase 27: Azure Policy

### Added
- **`Microsoft.Authorization/policyDefinitions` & `/policyAssignments`** — ARM CRUD (sync) across subscription and resource-group scopes.
- Preserves raw `policyRule` and `parameters` JSON without spurious diffs; built-in definitions return 404 to trigger custom definition creation.

---

## [0.26.0] — Phase 26: Azure Container Instances

### Added
- **`Microsoft.ContainerInstance/containerGroups`** — ARM CRUD (sync) with deterministic public IP and FQDN via SHA-256; `properties.sku` default satisfies `ForceNew` guard.

---

## [0.25.0] — Phase 25: Redis Cache

### Added
- **`Microsoft.Cache/redis`** — ARM CRUD (sync) with nested `properties.sku` matching `go-azure-sdk` shape to avoid zero-drift issues in `azurerm_redis_cache`.
- Deterministic hostname and primary/secondary keys via `listKeys`.

---

## [0.24.0] — Phase 24: Azure Container Registry

### Added
- **`Microsoft.ContainerRegistry/registries`** — ARM CRUD (sync), `loginServer` (`{name}.azurecr.io`), `checkNameAvailability`, and `listCredentials` admin credentials.
- Always-populated non-nil `networkRuleSet`, `policies`, and `encryption` objects to prevent provider panics.

---

## [0.23.0] — Phase 23: Azure SQL Database

### Added
- **`Microsoft.Sql/servers`, `databases`, `firewallRules`** — ARM CRUD (sync) and database PATCH support.
- Sub-resource fakes: `connectionPolicies` (singleton PUT/GET and collection GET), `restorableDroppedDatabases`, backup retention policies, `securityAlertPolicies`, and `transparentDataEncryption`.
- Accurate field mapping for `sku_name` and `storage_account_type` against `azurerm_mssql_*`.

---

## [0.21.0] — Phase 21: Logic Apps Recurrence Trigger

### Added
- **`Microsoft.Logic/workflows`** — ARM CRUD (sync) with single `Recurrence` trigger and single `Http` action.
- Background goroutine scheduler (`internal/cronlike`) that fires real scheduled HTTP POST requests and resumes automatically across server restarts.
- Synchronous manual execution via `POST .../triggers/{trigger}/run`.

---

## [0.20.0] — Phase 20: Action Groups Real Webhook Delivery

### Added
- Real HTTP POST delivery to `webhookReceivers` on `POST .../actionGroups/{name}/createNotifications` in Azure common-alert-schema format.
- Tracking of `lastNotificationTime` and `lastNotificationStatus`.

---

## [0.19.0] — Phase 19: ARM & Bicep Template Deployments

### Added
- **`Microsoft.Resources/deployments`** — Evaluates expressions (`parameters()`, `variables()`, `resourceId()`), orders resources by `dependsOn`, and synthesizes internal PUT requests across existing service handlers.
- `/operations` (sync) and `/validate` (dry-run).

---

## [0.18.0] — Phase 18: API Management

### Added
- **`Microsoft.ApiManagement/service`** — Service instance ARM CRUD (async) with deterministic gateway URLs.
- Sub-resources: `apis`, `operations`, `products`, `subscriptions` with deterministic keys.

---

## [0.17.0] — Phase 17: Eventing (Event Grid & Event Hubs)

### Added
- **`Microsoft.EventGrid/topics` & `eventSubscriptions`** — ARM CRUD (sync) and real outbound webhook HTTP POST dispatch on event publishing.
- **`Microsoft.EventHub/namespaces`** — Namespaces (async), event hubs, consumer groups, and flat offset-ordered messaging data plane.

---

## [0.16.0] — Phase 16: Managed Identities

### Added
- **`Microsoft.ManagedIdentity/userAssignedIdentities`** — User-assigned identities with deterministic IDs.
- System-assigned identity sub-object support on App Service, VMs, and AKS clusters.

---

## [0.15.0] — Phase 15: Entra ID & RBAC

### Added
- Microsoft Graph stub extensions: application registrations and service principals (`$filter` support).
- Custom role definitions and scope-isolated role assignments (`Microsoft.Authorization`).

---

## [0.14.0] — Phase 14: Azure Functions

### Added
- `Microsoft.Web/sites/functions` sub-resource, `syncfunctiontriggers`, and `host/default/listkeys` actions.

---

## [0.13.0] — Phase 13: Azure Kubernetes Service (AKS)

### Added
- `Microsoft.ContainerService/managedClusters` and independently routable `agentPools` sub-resource (async).
- Synthetic credentials via `listClusterUserCredential` and `listClusterAdminCredential`.

---

## [0.12.0] — Phase 12: Networking Extensions

### Added
- Network Security Groups (NSGs) + security rules.
- Public IP addresses with deterministic IP assignment.
- Load Balancers with frontend/backend/probe configurations.
- Route Tables + routes.
- Private DNS zones + record sets (A/CNAME).

---

## [0.11.0] — Phase 11: App Service

### Added
- App Service Plans (`Microsoft.Web/serverfarms`) and Web Apps (`Microsoft.Web/sites`).
- Lifecycle actions: `start`, `stop`, `restart`.
- `config/appsettings` StringDictionary sub-resource.

---

## [0.10.0] — Phase 10: Monitor & Log Analytics

### Added
- Log Analytics workspaces (`Microsoft.OperationalInsights/workspaces`) + `sharedKeys` action.
- Log Analytics Query data-plane stub (`POST /v1/workspaces/{id}/query`).
- Action Groups (`Microsoft.Insights/actionGroups`) and Metric Alerts (`Microsoft.Insights/metricAlerts`).

---

## [0.0.1] to [0.9.0] — Foundation & Core Services (Phases 0-9)

### Added
- **Phase 0 & 1**: Core HTTP server, logging, panic recovery, CORS, ARM resource ID parsing, LRO async operation helper (`Azure-AsyncOperation`/`Location`), BoltDB persistence, `/healthz`.
- **Phase 2**: Resource Manager fake subscriptions and resource group CRUD with async deletion.
- **Phase 3**: Storage Accounts ARM CRUD; Blob, Queue, and Table storage data-plane endpoints.
- **Phase 4**: Compute (VNets, subnets, network interfaces, managed disks, static VM image catalog, VM lifecycle).
- **Phase 5**: Key Vault ARM CRUD, secrets, keys, and certificates data-plane.
- **Phase 6**: Service Bus (namespaces, queues, topics, peek-lock messaging) and Cosmos DB (SQL API accounts, databases, containers, documents).
- **Phase 7**: Lightweight zero-build vanilla JS web console served at `/console/`.
- **Phase 8**: `az`/`azurerm` compatibility layer (ARM metadata endpoint `/metadata/endpoints`, OAuth token issuer, Microsoft Graph stub, self-signed TLS via `-tls`, ARM case-normalization middleware).
- **Phase 9**: Test suite per service, duplicate route registration tests, and GitHub Actions CI.
