# PoC: full Terraform lifecycle with the real `azurerm` provider

This document captures a real run (not simulated, not a unit test) of the
complete Terraform `init` → `plan` → `apply` → `destroy` cycle using the
official `hashicorp/azurerm` provider (v3.117.1) pointed at the emulator
via `environment = "custom"` + `metadata_host`. The configuration used is
[`terraform/azurerm-smoke-test/main.tf`](../terraform/azurerm-smoke-test/main.tf).

There is no generic HTTP provider or mocks involved: it's the same
`azurerm` binary used against real Azure, speaking HTTPS with the
emulator, resolving AAD tokens, custom cloud metadata, and real ARM
dispatch.

## What it provisions

- `data.azurerm_subscription.current` — reads the subscription via ARM.
- `azurerm_resource_group.test` — resource group `azurerm-smoke-rg` in `eastus`.
- `azurerm_resource_group_template_deployment.test` — an ARM deployment
  (`Microsoft.Resources/deployments`) that uses `parameters()`/`variables()`/
  `resourceId()` to create a storage account (`Microsoft.Storage/storageAccounts`),
  dynamically dispatched by `internal/services/deployments` to
  `internal/services/storageaccounts`.

## How to reproduce it

```bash
go build -o azure-emulator.exe ./cmd/azure-emulator
./azure-emulator.exe -tls
# trust the cert (see "Enabling HTTPS" in the README), e.g. on Windows:
#   certutil -user -addstore Root .azure-emulator-data\tls\cert.pem

cd terraform/azurerm-smoke-test
terraform init
terraform plan
terraform apply -auto-approve
terraform destroy -auto-approve
```

## Real run — captured output

### `terraform init`

```
Initializing provider plugins found in the configuration...
- Reusing previous version of hashicorp/azurerm from the dependency lock file
- Using previously-installed hashicorp/azurerm v3.117.1

Initializing the backend...

Terraform has been successfully initialized!
```

### `terraform plan`

```
data.azurerm_subscription.current: Reading...
data.azurerm_subscription.current: Read complete after 0s [id=/subscriptions/00000000-0000-0000-0000-000000000000]

Terraform used the selected providers to generate the following execution
plan. Resource actions are indicated with the following symbols:
  + create

Terraform will perform the following actions:

  # azurerm_resource_group.test will be created
  + resource "azurerm_resource_group" "test" {
      + id       = (known after apply)
      + location = "eastus"
      + name     = "azurerm-smoke-rg"
    }

  # azurerm_resource_group_template_deployment.test will be created
  + resource "azurerm_resource_group_template_deployment" "test" {
      + deployment_mode     = "Incremental"
      + id                  = (known after apply)
      + name                = "azurerm-smoke-deployment"
      + output_content      = (known after apply)
      + parameters_content  = (known after apply)
      + resource_group_name = "azurerm-smoke-rg"
      + template_content    = jsonencode(
            {
              + "$schema"      = "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"
              + contentVersion = "1.0.0.0"
              + outputs        = {
                  + storageId = {
                      + type  = "string"
                      + value = "[resourceId('Microsoft.Storage/storageAccounts', parameters('storageName'))]"
                    }
                }
              + parameters     = {
                  + storageName = {
                      + defaultValue = "azurermsmokedeploystg"
                      + type         = "string"
                    }
                }
              + resources      = [
                  + {
                      + apiVersion = "2023-01-01"
                      + location   = "eastus"
                      + name       = "[parameters('storageName')]"
                      + sku        = {
                          + name = "[variables('skuName')]"
                        }
                      + type       = "Microsoft.Storage/storageAccounts"
                    },
                ]
              + variables      = {
                  + skuName = "Standard_LRS"
                }
            }
        )
    }

Plan: 2 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + deployment_id     = (known after apply)
  + resource_group_id = (known after apply)
  + subscription_id   = "00000000-0000-0000-0000-000000000000"
```

### `terraform apply -auto-approve`

```
azurerm_resource_group.test: Creating...
azurerm_resource_group.test: Creation complete after 8s [id=/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg]
azurerm_resource_group_template_deployment.test: Creating...
azurerm_resource_group_template_deployment.test: Creation complete after 0s [id=/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg/providers/Microsoft.Resources/deployments/azurerm-smoke-deployment]

Apply complete! Resources: 2 added, 0 changed, 0 destroyed.

Outputs:

deployment_id = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg/providers/Microsoft.Resources/deployments/azurerm-smoke-deployment"
resource_group_id = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg"
subscription_id = "00000000-0000-0000-0000-000000000000"
```

### `terraform destroy -auto-approve`

```
azurerm_resource_group_template_deployment.test: Destroying... [id=/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg/providers/Microsoft.Resources/deployments/azurerm-smoke-deployment]
azurerm_resource_group_template_deployment.test: Destruction complete after 0s
azurerm_resource_group.test: Destroying... [id=/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/azurerm-smoke-rg]
azurerm_resource_group.test: Destruction complete after 0s

Destroy complete! Resources: 2 destroyed.
```

## Real bugs found and fixed during this test

This live run wasn't clean on the first try. The real `azurerm` provider
exercises ARM template-deployment lifecycle paths that no unit test
covered yet. Three real bugs were found and fixed in
`internal/services/deployments` and `internal/services/resourcemanager`:

1. **`POST .../deployments/{name}/exportTemplate` was missing.** After
   creating the deployment, the provider calls this endpoint to read back
   `template_content` and store it in state. Since the route didn't
   exist, the request fell through to the data-plane wildcard in
   `registerDataPlane` (`cmd/azure-emulator/main.go`) and returned a
   generic 404 with no apparent relation to deployments. Added the route,
   an `exportTemplate` handler, and a new BoltDB bucket
   (`deployments.templates`) to persist the raw template JSON received in
   the original `PUT`.

2. **`properties.providers` came back `nil`.** On destroy, the provider
   reads `properties.providers` from the deployment's `GET` response to
   know which namespaces/resourceTypes it touched so it can clean them
   up. Without that field, `terraform destroy` failed with
   *"properties.Providers was nil - insufficient data to clean up this
   Template Deployment"*. Added a `Providers []DeploymentProvider` field
   to `DeploymentProperties`, plus `buildProviders()`/`splitResourceType()`
   helpers to derive it automatically from the resources resolved by the
   dispatcher on each `PUT`.

3. **`ProviderResourceType` didn't expose `apiVersions`.** During
   cleanup, the provider calls `GET /subscriptions/{id}/providers/{namespace}`
   to pick which apiVersion to use when deleting each resourceType.
   Without that field, it failed with *"unable to determine API version
   for Resource Type \"storageAccounts\""*. Added `ApiVersions []string`
   to `ProviderResourceType` and populated it with a reasonable version
   for every resourceType already registered in `registeredNamespaces`
   (`internal/services/resourcemanager/resourcemanager.go`), not just
   `Microsoft.Storage`, to prevent the same issue from recurring with any
   other namespace in future tests.

None of these three bugs were visible with the generic HTTP provider or
with the existing unit tests — they only showed up when exercising the
full lifecycle (`apply` + `destroy`) with the real provider, which is
exactly the point of this test.

After the three fixes, the full run was repeated from a clean state
(`.azure-emulator-data` deleted, cert reissued and re-trusted, clean
Terraform state) and all four steps — `init`, `plan`, `apply`, `destroy` —
completed cleanly, with the real output shown above.
