# PoC: ciclo de vida completo de Terraform con el provider real `azurerm`

Este documento captura una corrida real (no simulada, no unit-test) del
ciclo completo `init` → `plan` → `apply` → `destroy` de Terraform usando el
provider oficial `hashicorp/azurerm` (v3.117.1) apuntando al emulador vía
`environment = "custom"` + `metadata_host`. La configuración usada es
[`terraform/azurerm-smoke-test/main.tf`](../terraform/azurerm-smoke-test/main.tf).

No hay ningún provider HTTP genérico ni mocks de por medio: es el mismo
binario de `azurerm` que se usa contra Azure real, hablando HTTPS con el
emulador, resolviendo AAD tokens, metadata de cloud custom y dispatch ARM
real.

## Qué provisiona

- `data.azurerm_subscription.current` — lee la suscripción vía ARM.
- `azurerm_resource_group.test` — resource group `azurerm-smoke-rg` en `eastus`.
- `azurerm_resource_group_template_deployment.test` — un deployment ARM
  (`Microsoft.Resources/deployments`) que usa `parameters()`/`variables()`/
  `resourceId()` para crear una storage account (`Microsoft.Storage/storageAccounts`),
  dispatchado dinámicamente por `internal/services/deployments` hacia
  `internal/services/storageaccounts`.

## Cómo reproducirlo

```bash
go build -o azure-emulator.exe ./cmd/azure-emulator
./azure-emulator.exe -tls
# confiar en el cert (ver "Enabling HTTPS" en el README), p. ej. en Windows:
#   certutil -user -addstore Root .azure-emulator-data\tls\cert.pem

cd terraform/azurerm-smoke-test
terraform init
terraform plan
terraform apply -auto-approve
terraform destroy -auto-approve
```

## Corrida real — salida capturada

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

## Bugs reales encontrados y corregidos durante esta prueba

Esta corrida live no salió limpia a la primera. El provider real `azurerm`
ejercita rutas del ciclo de vida de un ARM template deployment que ningún
test unitario cubría todavía. Se encontraron y corrigieron tres bugs reales
en `internal/services/deployments` y `internal/services/resourcemanager`:

1. **Faltaba `POST .../deployments/{name}/exportTemplate`.** Tras crear el
   deployment, el provider llama a este endpoint para leer de vuelta el
   `template_content` y guardarlo en el state. Al no existir la ruta, el
   request caía en el wildcard de data-plane de `registerDataPlane`
   (`cmd/azure-emulator/main.go`) y devolvía un 404 genérico sin relación
   aparente con deployments. Se agregó la ruta, un handler `exportTemplate`,
   y un bucket nuevo (`deployments.templates`) en BoltDB para persistir el
   JSON crudo del template recibido en el `PUT` original.

2. **`properties.providers` venía `nil`.** Al destruir, el provider lee
   `properties.providers` de la respuesta `GET` del deployment para saber
   qué namespaces/resourceTypes tocó y poder limpiarlos. Sin ese campo,
   `terraform destroy` fallaba con *"properties.Providers was nil -
   insufficient data to clean up this Template Deployment"*. Se agregó el
   campo `Providers []DeploymentProvider` a `DeploymentProperties`, más
   `buildProviders()`/`splitResourceType()` para derivarlo automáticamente
   de los recursos resueltos por el dispatcher en cada `PUT`.

3. **`ProviderResourceType` no exponía `apiVersions`.** Durante el cleanup,
   el provider llama a `GET /subscriptions/{id}/providers/{namespace}` para
   elegir qué apiVersion usar al borrar cada resourceType. Al no existir el
   campo, fallaba con *"unable to determine API version for Resource Type
   \"storageAccounts\""*. Se agregó `ApiVersions []string` a
   `ProviderResourceType` y se pobló con una versión razonable para cada
   resourceType ya registrado en `registeredNamespaces`
   (`internal/services/resourcemanager/resourcemanager.go`), no solo para
   `Microsoft.Storage`, para evitar que el mismo problema reaparezca con
   cualquier otro namespace en pruebas futuras.

Ninguno de estos tres bugs era visible con el provider HTTP genérico ni con
los tests unitarios existentes — solo aparecieron al ejercitar el ciclo de
vida completo (`apply` + `destroy`) con el provider real, que es exactamente
el punto de esta prueba.

Tras las tres correcciones, se repitió la corrida completa desde cero
(`.azure-emulator-data` borrado, cert reemitido y re-confiado, state de
Terraform limpio) y los cuatro pasos —`init`, `plan`, `apply`, `destroy`—
terminaron en verde, con la salida real mostrada arriba.
