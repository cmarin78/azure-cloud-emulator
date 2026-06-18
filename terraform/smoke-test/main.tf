# Smoke-test de Terraform contra el emulador.
#
# IMPORTANTE: esto NO usa el provider `azurerm` real. El provider azurerm
# hace descubrimiento de metadata ARM (GET https://<endpoint>/metadata/endpoints)
# y requiere un token de Azure AD válido antes de emitir un solo request —
# ninguna de las dos cosas las implementa (todavía) este emulador. Apuntar
# azurerm directamente a localhost fallará en el paso de autenticación,
# antes de llegar a nuestras rutas.
#
# Mientras eso no esté implementado (ver ROADMAP.md), este smoke-test usa
# el provider genérico `http` para verificar que las rutas REST responden
# con la forma esperada — útil para CI/regresión, no para "terraform apply"
# real de recursos azurerm.

terraform {
  required_providers {
    http = {
      source  = "hashicorp/http"
      version = "~> 3.4"
    }
  }
}

variable "endpoint" {
  type    = string
  default = "http://localhost:10000"
}

variable "subscription_id" {
  type    = string
  default = "00000000-0000-0000-0000-000000000000"
}

variable "resource_group" {
  type    = string
  default = "tf-smoke-rg"
}

variable "storage_account" {
  type    = string
  default = "tfsmokestorage"
}

# PUT resource group (data source con `http` no soporta PUT, así que usamos
# un null_resource + curl local-exec para la escritura, y `http` solo para
# los GET de verificación).
resource "null_resource" "resource_group" {
  triggers = {
    rg = var.resource_group
  }

  provisioner "local-exec" {
    command = <<-EOT
      curl -sf -X PUT "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}?api-version=2021-04-01" \
        -H "Content-Type: application/json" \
        -d '{"location": "eastus"}'
    EOT
  }
}

resource "null_resource" "storage_account" {
  depends_on = [null_resource.resource_group]
  triggers = {
    account = var.storage_account
  }

  provisioner "local-exec" {
    command = <<-EOT
      curl -sf -X PUT "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Storage/storageAccounts/${var.storage_account}?api-version=2023-01-01" \
        -H "Content-Type: application/json" \
        -d '{"location": "eastus", "sku": {"name": "Standard_LRS"}, "kind": "StorageV2"}'
    EOT
  }
}

# Verificación de lectura vía el provider `http` (este sí es un GET real
# hecho por Terraform, no un local-exec).
data "http" "resource_group" {
  depends_on = [null_resource.resource_group]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}?api-version=2021-04-01"
}

data "http" "storage_account" {
  depends_on = [null_resource.storage_account]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Storage/storageAccounts/${var.storage_account}?api-version=2023-01-01"
}

output "resource_group_response" {
  value = jsondecode(data.http.resource_group.response_body)
}

output "storage_account_response" {
  value = jsondecode(data.http.storage_account.response_body)
}
