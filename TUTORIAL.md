# Azure Emulator — Getting Started Tutorial

This is a hands-on walkthrough for installing, running, and testing the
Azure Emulator — a local, dependency-free stand-in for Azure's
management and data-plane REST APIs. By the end you'll have it running
on `localhost:10000`, browsing resources in the web console, driving it
with real `az` CLI commands, and provisioning resources with Terraform
(both the generic `http` provider and the real `azurerm` provider).

No real Azure subscription, billing, or network access is required for
any of this.

## Table of contents

1. [Prerequisites](#1-prerequisites)
2. [Install and build](#2-install-and-build)
3. [Run it](#3-run-it)
4. [Try the web console](#4-try-the-web-console)
5. [Test with az CLI](#5-test-with-az-cli)
6. [Test with Terraform (generic http provider)](#6-test-with-terraform-generic-http-provider)
7. [Test with Terraform (real azurerm provider)](#7-test-with-terraform-real-azurerm-provider)
8. [Running with Docker instead](#8-running-with-docker-instead)
9. [Troubleshooting](#9-troubleshooting)

---

## 1. Prerequisites

You'll need:

- **Go 1.22+** — only to build from source. Get it from
  [go.dev/dl](https://go.dev/dl/), or `winget install GoLang.Go` on
  Windows, `brew install go` on macOS, `apt install golang-go` on Linux.
- **Azure CLI** (`az`) — optional, only needed for Section 5.
- **Terraform** — optional, only needed for Sections 6 and 7.
- **Docker** — optional, only needed for Section 8.

Check what you have:

```bash
go version
az version
terraform version
```

## 2. Install and build

Clone the repo and fetch dependencies:

```bash
git clone https://github.com/cmarin78/azure-cloud-emulator.git azure-emulator
cd azure-emulator
go mod tidy
```

You can either run it directly with `go run`, or build a standalone
binary:

```bash
go build -o azure-emulator ./cmd/azure-emulator
# Windows: go build -o azure-emulator.exe ./cmd/azure-emulator
```

## 3. Run it

The simplest way:

```bash
go run ./cmd/azure-emulator
```

You should see it start listening. By default:

- Listens on `:10000`
- Persists state to `.azure-emulator-data/azure-emulator.db` (a single
  embedded BoltDB file — delete this directory any time to reset to a
  clean slate)

Confirm it's alive:

```bash
curl http://localhost:10000/healthz
```

Useful flags / environment variables, if you want to override the
defaults:

| Flag | Env var | Default | Purpose |
|---|---|---|---|
| `-addr` | `AZURE_EMULATOR_ADDR` | `:10000` | Listen address |
| `-db` | `AZURE_EMULATOR_DB` | `.azure-emulator-data/azure-emulator.db` | BoltDB file path |
| `-web` | `AZURE_EMULATOR_WEB` | `web/console` | Web console static files directory |
| `-tls` | — | off | Enable self-signed HTTPS (needed for az CLI/azurerm real auth — see Section 7) |

Example with a custom port and a throwaway database:

```bash
go run ./cmd/azure-emulator -addr :8080 -db /tmp/azure-emulator-demo.db
```

Leave this process running in its own terminal for the rest of this
tutorial.

## 4. Try the web console

With the emulator running, open:

```
http://localhost:10000/
```

This is a minimal vanilla HTML/CSS/JS console (no build step, no
separate server — the binary itself serves it) for browsing what's
been created so far: resource groups, storage accounts, virtual
machines, Key Vault vaults, Service Bus namespaces, and Cosmos DB
accounts. It's read/list plus create/delete for most of those (VMs are
list/start/stop/delete only, since creating one needs a pre-existing
NIC/disk).

Practical example — create a resource group from the console:

1. Open `http://localhost:10000/`
2. Find the "Resource Groups" panel
3. Enter a name (e.g. `console-demo-rg`) and a location (e.g.
   `eastus`), click **Create**
4. It should immediately appear in the list — the console is just
   calling the same REST API you'll use directly in the next sections

If the console doesn't load, see [Troubleshooting](#9-troubleshooting).

## 5. Test with az CLI

### 5.1 The one thing to know up front

Azure CLI normally authenticates against real Azure AD before it'll
let you call any endpoint, and its MSAL library refuses to treat
`localhost` as a valid authority during that process (a client-side
restriction, not something the emulator can work around). The
practical fix: log in once with **any** real Azure account — the token
itself is never validated by the emulator — and then use
[`az rest`](https://learn.microsoft.com/cli/azure/reference-index#az-rest)
to point individual calls at `localhost` directly.

```bash
az login   # any account/tenant works — only needed once per session
```

### 5.2 Run the full smoke-test script

The repo ships a script that exercises essentially everything
implemented so far end to end:

```bash
# macOS/Linux
./scripts/test-az-cli.sh

# Windows
.\scripts\test-az-cli.ps1
```

This walks through resource group CRUD, storage (accounts, blobs,
queues, tables), networking and VMs, Key Vault, Service Bus, Cosmos
DB, Monitor/Log Analytics, App Service, additional networking (NSGs,
Load Balancers, Route Tables, Private DNS), AKS, and Functions —
printing each response as it goes. It's the fastest way to see that a
fresh checkout is fully working.

### 5.3 Practical example: resource group + storage account by hand

Once you've logged in, you can drive the emulator with individual
`az rest` calls. Here's a small end-to-end example outside the script:

```bash
ENDPOINT="http://localhost:10000"
SUB="00000000-0000-0000-0000-000000000000"
RG="my-test-rg"

# Create a resource group
az rest --method put \
  --url "$ENDPOINT/subscriptions/$SUB/resourceGroups/$RG?api-version=2021-04-01" \
  --body '{"location": "eastus"}'

# Read it back
az rest --method get \
  --url "$ENDPOINT/subscriptions/$SUB/resourceGroups/$RG?api-version=2021-04-01"

# Create a storage account in it
az rest --method put \
  --url "$ENDPOINT/subscriptions/$SUB/resourceGroups/$RG/providers/Microsoft.Storage/storageAccounts/mydemostorage?api-version=2022-09-01" \
  --body '{"location": "eastus", "sku": {"name": "Standard_LRS"}, "kind": "StorageV2"}'

# List storage accounts in the resource group
az rest --method get \
  --url "$ENDPOINT/subscriptions/$SUB/resourceGroups/$RG/providers/Microsoft.Storage/storageAccounts?api-version=2022-09-01"

# Clean up (cascading async delete)
az rest --method delete \
  --url "$ENDPOINT/subscriptions/$SUB/resourceGroups/$RG?api-version=2021-04-01"
```

That same `az rest --method put/get/post/delete --url <endpoint>/...`
pattern works for every resource type in the project — swap the path
and body to match the resource you want (see `scripts/test-az-cli.sh`/
`.ps1` for a complete catalog of working examples for every service).

## 6. Test with Terraform (generic `http` provider)

This is the fastest way to provision a wide variety of resources in
one shot without worrying about real Azure authentication at all — it
uses Terraform's generic `http` provider plus `local-exec` calls, so it
talks to the emulator exactly like `curl` would, just declaratively.

```bash
cd terraform/smoke-test
terraform init
terraform apply
```

This single `apply` provisions, against your running emulator: a
resource group, a storage account (with a blob container/blob, a
queue + message, a table + entity), a virtual network/subnet/NIC/disk/
VM, a Key Vault (with a secret/key/certificate), a Service Bus
namespace (with a queue + message), a Cosmos DB account (with a
database/container/document), a Log Analytics workspace + action
group + metric alert, an App Service Plan + Web App + app settings,
NSGs/Public IPs/Load Balancer/Route Table/Private DNS, an AKS managed
cluster + agent pool, and a Functions App Service Plan + Function App
+ function definition.

Practical example — inspect what got created:

```bash
terraform output                       # see every output at once
terraform output cosmos_account_response
terraform output vm_response
terraform output func_app_response
```

Override any default without touching the file:

```bash
terraform apply \
  -var="endpoint=http://localhost:10000" \
  -var="resource_group=my-test-rg" \
  -var="location=westus2"
```

Preview changes after editing `main.tf` or bumping an `api-version`:

```bash
terraform plan
```

Tear down the Terraform-tracked state:

```bash
terraform destroy
```

One important caveat: `terraform destroy` here only removes
Terraform's own tracked `null_resource`/`data` entries — it does
**not** delete the underlying resources inside the emulator, since
they were created via `local-exec` `Invoke-RestMethod`/`curl` calls
rather than real Terraform-managed resources. To actually clear
emulator state, either run the matching `DELETE` calls (see
`scripts/test-az-cli.sh`/`.ps1`) or just restart the emulator pointed
at a fresh `-db` path.

## 7. Test with Terraform (real `azurerm` provider)

This is the more realistic test: it points the actual
`hashicorp/azurerm` Terraform provider — the same one you'd use against
real Azure — at the emulator, proving ARM metadata discovery, fake AAD
token issuance, and resource create/destroy all work without any
provider patches.

### 7.1 Start the emulator with TLS enabled

Both az CLI's and `azurerm`'s HTTP stacks refuse to treat a custom
cloud as valid over plain HTTP, so this mode requires `-tls`:

```bash
go run ./cmd/azure-emulator -tls
```

On first run this generates a self-signed cert/key pair under
`.azure-emulator-data/tls/` and reuses it afterward.

### 7.2 Trust the self-signed certificate

```powershell
# Windows
certutil -addstore Root .azure-emulator-data\tls\cert.pem
```

```bash
# macOS
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain .azure-emulator-data/tls/cert.pem
```

```bash
# Linux (Debian/Ubuntu)
sudo cp .azure-emulator-data/tls/cert.pem \
  /usr/local/share/ca-certificates/azure-emulator.crt
sudo update-ca-certificates
```

`azurerm`'s Go HTTP stack reads the OS certificate store directly, so
this step alone is enough for Terraform.

### 7.3 Apply and destroy

```bash
cd terraform/azurerm-smoke-test
terraform init
terraform apply
```

This provisions a real `azurerm_resource_group` against the emulator
and reads the (fake) subscription via `data "azurerm_subscription"`,
confirming the full chain works: metadata discovery →
fake AAD token → service-principal object-ID resolution (via the
emulator's Microsoft Graph stub) → resource create.

```bash
terraform destroy
```

### 7.4 Practical example: add your own azurerm resource

Once the basic flow works, you can extend `terraform/azurerm-smoke-test/
main.tf` with any other `azurerm_*` resource the emulator implements —
for example, a storage account:

```hcl
resource "azurerm_storage_account" "demo" {
  name                     = "myazurermdemo"
  resource_group_name      = azurerm_resource_group.demo.name
  location                 = azurerm_resource_group.demo.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}
```

Then `terraform apply` again from the same directory.

## 8. Running with Docker instead

If you'd rather not install Go locally:

```bash
docker compose up --build
```

This builds a multi-stage image and starts a container listening on
`localhost:10000`, persisting its BoltDB file to a named volume
(`emulator-data`) so data survives container restarts. Everything in
Sections 4–7 works exactly the same against this container as it does
against `go run`.

Without compose:

```bash
docker build -t azure-emulator:local .
docker run --rm -p 10000:10000 -v emulator-data:/data azure-emulator:local
```

## 9. Troubleshooting

**`curl: (7) Failed to connect to localhost port 10000`**
The emulator isn't running, or it's listening on a different port/
address. Check the terminal where you ran `go run`/`go build` for
startup errors, and confirm `-addr`/`AZURE_EMULATOR_ADDR` if you
customized it.

**Web console shows a blank page or 404s on its assets**
Confirm the `web/console` directory exists relative to where you
started the binary (or pass `-web <path>`/set `AZURE_EMULATOR_WEB`).
If that directory is missing, the console is silently disabled and
only the REST API is served.

**`az login` fails with `invalid_instance: ... localhost ... is not known`**
This is MSAL's instance-discovery check rejecting `localhost` as an
authority — it happens if you try `az login --service-principal`/
`az cloud register` against a custom cloud pointed at the emulator.
Stick with the documented flow instead: `az login` once with **any**
real account, then drive the emulator with `az rest --url <endpoint>/...`
(see Section 5.1). If you'd previously set up a custom `az cloud` for
this and it's now your active cloud, switch back first:

```bash
az cloud set --name AzureCloud
az login
```

**Terraform (`azurerm` provider) can't connect or fails TLS verification**
Make sure the emulator was started with `-tls` and that you completed
the certificate-trust step in 7.2 for your OS. Re-running
`terraform init`/`apply` after trusting the cert (rather than relying
on a cached provider plugin state) usually clears this up.

**Resources from a previous test run keep causing `Conflict`/`AlreadyExists` errors**
The emulator persists state across restarts in
`.azure-emulator-data/azure-emulator.db`. Stop the emulator and delete
that directory (or pass a fresh `-db` path) to start from a clean
slate.

**Where to look for exact request/response shapes**
`scripts/test-az-cli.sh`/`.ps1` and `terraform/smoke-test/main.tf` are
living, continuously-verified examples of every resource type the
emulator supports — copy the relevant block from either when you want
the precise URL, `api-version`, and body shape for something not
covered above. `README.md`/`ROADMAP.md` describe what's implemented
phase by phase.
