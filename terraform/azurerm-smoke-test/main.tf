# Smoke-test del provider REAL `azurerm` (no el provider genérico `http`
# usado en terraform/smoke-test/main.tf) contra el emulador.
#
# Esto requiere:
#   1. El emulador corriendo con `-tls` (HTTPS): tanto el descubrimiento de
#      metadata ARM como MSAL/azurerm rechazan un cloud personalizado sobre
#      HTTP plano. Ver "Habilitar HTTPS" en README.md para los pasos de
#      generar/confiar en el certificado autofirmado.
#   2. Confiar en ese certificado autofirmado en el almacén de Windows
#      (certutil -addstore Root) y, si Go usa su propio almacén de CAs vía
#      variables de entorno en este shell, exportar SSL_CERT_FILE/
#      REQUESTS_CA_BUNDLE apuntando al bundle combinado (cert del emulador +
#      CAs públicas).
#
# A diferencia de az CLI (`az login --service-principal`), que sigue
# bloqueado por el chequeo de "instance discovery" de MSAL (rechaza
# "localhost" como authority sin un flag para desactivarlo en esta versión
# de az CLI — ver README.md, sección "Limitaciones conocidas"), el stack de
# autenticación en Go de `azurerm` NO hace ese chequeo, así que login +
# descubrimiento de metadata + emisión de token + llamadas ARM funcionan
# de punta a punta contra el emisor de tokens falso de
# internal/services/aadtoken y el documento de metadata de
# internal/services/armmeta.
#
# El único endpoint adicional que hizo falta para esto (más allá de ARM
# mismo) es un Microsoft Graph mínimo (internal/services/graph): azurerm
# resuelve el object ID del service principal autenticado vía
# GET /v1.0/servicePrincipals?$filter=appId eq '{clientId}' cuando el
# access token no trae el claim "oid" — que es siempre el caso aquí, ya que
# el emisor de tokens falso no simula ningún directorio real.

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

variable "metadata_host" {
  type    = string
  default = "localhost:10000"
}

variable "subscription_id" {
  type    = string
  default = "00000000-0000-0000-0000-000000000000"
}

variable "tenant_id" {
  type    = string
  default = "72f988bf-86f1-41af-91ab-2d7cd011db47"
}

variable "resource_group" {
  type    = string
  default = "azurerm-smoke-rg"
}

variable "location" {
  type    = string
  default = "eastus"
}

provider "azurerm" {
  features {}

  environment     = "custom"
  metadata_host   = var.metadata_host
  client_id       = "fake-client"
  client_secret   = "fake-secret"
  tenant_id       = var.tenant_id
  subscription_id = var.subscription_id

  # El emulador acepta cualquier client_id/secret/tenant_id sin validarlos
  # de verdad (ver internal/services/aadtoken) — no hay un directorio real
  # detrás, así que tampoco tiene sentido que azurerm intente registrar
  # providers contra él en el sentido real de Azure.
  skip_provider_registration = true
}

data "azurerm_subscription" "current" {}

resource "azurerm_resource_group" "test" {
  name     = var.resource_group
  location = var.location
}

output "subscription_id" {
  value = data.azurerm_subscription.current.subscription_id
}

output "resource_group_id" {
  value = azurerm_resource_group.test.id
}
