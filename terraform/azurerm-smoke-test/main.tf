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

# Fase 19 (ARM/Bicep deployments): el provider azurerm real expone
# Microsoft.Resources/deployments como azurerm_resource_group_template_
# deployment. El template usa parameters()/resourceId() para crear una
# storage account real vía el dispatcher de internal/services/deployments
# — mismo template que prueba TestDeploymentDispatchesResourceAndPersists
# Operations en internal/services/deployments/deployments_test.go.
resource "azurerm_resource_group_template_deployment" "test" {
  name                = "azurerm-smoke-deployment"
  resource_group_name = azurerm_resource_group.test.name
  deployment_mode     = "Incremental"

  template_content = jsonencode({
    "$schema"      = "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"
    contentVersion = "1.0.0.0"
    parameters = {
      storageName = {
        type         = "string"
        defaultValue = "azurermsmokedeploystg"
      }
    }
    variables = {
      skuName = "Standard_LRS"
    }
    resources = [
      {
        type       = "Microsoft.Storage/storageAccounts"
        apiVersion = "2023-01-01"
        name       = "[parameters('storageName')]"
        location   = var.location
        sku        = { name = "[variables('skuName')]" }
      }
    ]
    outputs = {
      storageId = {
        type  = "string"
        value = "[resourceId('Microsoft.Storage/storageAccounts', parameters('storageName'))]"
      }
    }
  })
}

# Fase 23 (SQL Database): server + database + firewall rule. El provider
# azurerm real construye server_id/id a partir de la respuesta del PUT (no
# hace falta construirlo a mano como en terraform/smoke-test/main.tf, que
# usa el provider genérico `http`).
resource "azurerm_mssql_server" "test" {
  name                         = "azurermsmokesqlsrv"
  resource_group_name          = azurerm_resource_group.test.name
  location                     = var.location
  version                      = "12.0"
  administrator_login          = "sqladmin"
  administrator_login_password = "P@ssw0rd1234!"
}

resource "azurerm_mssql_database" "test" {
  name      = "azurermsmokedb"
  server_id = azurerm_mssql_server.test.id
  sku_name  = "Basic"
}

resource "azurerm_mssql_firewall_rule" "test" {
  name             = "AllowAll"
  server_id        = azurerm_mssql_server.test.id
  start_ip_address = "0.0.0.0"
  end_ip_address   = "255.255.255.255"
}

# Fase 24 (Container Registry).
resource "azurerm_container_registry" "test" {
  name                = "azurermsmokeacr"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  sku                 = "Basic"
  admin_enabled       = true
}

# Fase 26 (Container Instances).
resource "azurerm_container_group" "test" {
  name                = "azurermsmokeci"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  ip_address_type     = "Public"
  os_type             = "Linux"

  container {
    name   = "mycontainer"
    image  = "nginx:latest"
    cpu    = "1"
    memory = "1.5"

    ports {
      port     = 80
      protocol = "TCP"
    }
  }
}

# Fase 27 (Azure Policy).
resource "azurerm_policy_definition" "test" {
  name         = "deny-public-ip-smoke"
  policy_type  = "Custom"
  mode         = "All"
  display_name = "Deny public IPs (smoke)"

  policy_rule = jsonencode({
    if = {
      field  = "type"
      equals = "Microsoft.Network/publicIPAddresses"
    }
    then = {
      effect = "deny"
    }
  })
}

resource "azurerm_subscription_policy_assignment" "test" {
  name                 = "deny-public-ip-assign"
  policy_definition_id = azurerm_policy_definition.test.id
  subscription_id      = "/subscriptions/${var.subscription_id}"
  display_name         = "Deny public IPs assignment (smoke)"
  enforce              = false
}

# Fase 29 (Static Web Apps).
resource "azurerm_static_web_app" "test" {
  name                = "azurermsmokestaticsite"
  resource_group_name = azurerm_resource_group.test.name
  location            = "eastus2"
  sku_tier            = "Free"
  sku_size            = "Free"
}

# Fase 28 (Storage File Shares).
resource "azurerm_storage_account" "fileshare_test" {
  name                     = "azurermsmokefsacct"
  resource_group_name      = azurerm_resource_group.test.name
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_storage_share" "test" {
  name                 = "azurermsmokefileshare"
  storage_account_name = azurerm_storage_account.fileshare_test.name
  quota                = 50
}

# Fase 32 (Notification Hubs).
resource "azurerm_notification_hub_namespace" "test" {
  name                = "azurermsmokenh"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  namespace_type      = "NotificationHub"
  sku_name            = "Free"
}

resource "azurerm_notification_hub" "test" {
  name                    = "azurermsmokehub"
  namespace_name          = azurerm_notification_hub_namespace.test.name
  resource_group_name     = azurerm_resource_group.test.name
  location                = var.location
}

# Fase 32 (SignalR Service).
resource "azurerm_signalr_service" "test" {
  name                = "azurermsmokesr"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location

  sku {
    name     = "Free_F1"
    capacity = 1
  }
}

# Fase 30 (PostgreSQL Flexible Server).
resource "azurerm_postgresql_flexible_server" "test" {
  name                   = "azurermsmokepostgres"
  resource_group_name    = azurerm_resource_group.test.name
  location               = var.location
  version                = "16"
  administrator_login    = "pgadmin"
  administrator_password = "P@ssw0rd1234!"
  storage_mb             = 32768
  sku_name               = "B_Standard_B1ms"
  backup_retention_days  = 7
}

resource "azurerm_postgresql_flexible_server_database" "test" {
  name      = "azurermsmokepgdb"
  server_id = azurerm_postgresql_flexible_server.test.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

resource "azurerm_postgresql_flexible_server_firewall_rule" "test" {
  name             = "AllowAll"
  server_id        = azurerm_postgresql_flexible_server.test.id
  start_ip_address = "0.0.0.0"
  end_ip_address   = "255.255.255.255"
}

# Fase 30 (MySQL Flexible Server).
resource "azurerm_mysql_flexible_server" "test" {
  name                   = "azurermsmokemysql"
  resource_group_name    = azurerm_resource_group.test.name
  location               = var.location
  administrator_login    = "mysqladmin"
  administrator_password = "P@ssw0rd1234!"
  sku_name               = "B_Standard_B1ms"
  version                = "8.0.21"
  backup_retention_days  = 7
}

resource "azurerm_mysql_flexible_database" "test" {
  name                = "azurermsmokemydb"
  resource_group_name = azurerm_resource_group.test.name
  server_name         = azurerm_mysql_flexible_server.test.name
  charset             = "utf8mb4"
  collation           = "utf8mb4_unicode_ci"
}

# Fase 31 (Recovery Services Vault).
resource "azurerm_recovery_services_vault" "test" {
  name                = "azurermsmokersvault"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  sku                 = "Standard"
}

# Fase 25 (Redis Cache).
resource "azurerm_redis_cache" "test" {
  name                = "azurermsmokereddis"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  capacity            = 1
  family              = "C"
  sku_name            = "Standard"
  enable_non_ssl_port = false
  minimum_tls_version = "1.2"
}

# Fase 33 (Private Endpoints + Private Link Services).
# azurerm_private_endpoint necesita un subnet ID y un private_service_connection
# con el ID del recurso destino. Aquí apuntamos al storage account de File Shares
# (azurerm_storage_account.fileshare_test) ya declarado arriba.
resource "azurerm_virtual_network" "pe_vnet" {
  name                = "azurermsmokepe-vnet"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  address_space       = ["10.10.0.0/16"]
}

resource "azurerm_subnet" "pe_subnet" {
  name                 = "pe-subnet"
  resource_group_name  = azurerm_resource_group.test.name
  virtual_network_name = azurerm_virtual_network.pe_vnet.name
  address_prefixes     = ["10.10.1.0/24"]
}

resource "azurerm_private_endpoint" "test" {
  name                = "azurermsmokependpoint"
  resource_group_name = azurerm_resource_group.test.name
  location            = var.location
  subnet_id           = azurerm_subnet.pe_subnet.id

  private_service_connection {
    name                           = "conn1"
    private_connection_resource_id = azurerm_storage_account.fileshare_test.id
    subresource_names              = ["blob"]
    is_manual_connection           = false
  }
}

output "subscription_id" {
  value = data.azurerm_subscription.current.subscription_id
}

output "resource_group_id" {
  value = azurerm_resource_group.test.id
}

output "deployment_id" {
  value = azurerm_resource_group_template_deployment.test.id
}

output "sql_server_fqdn" {
  value = azurerm_mssql_server.test.fully_qualified_domain_name
}

output "sql_database_id" {
  value = azurerm_mssql_database.test.id
}

output "acr_login_server" {
  value = azurerm_container_registry.test.login_server
}

output "container_group_ip" {
  value = azurerm_container_group.test.ip_address
}

output "container_group_fqdn" {
  value = azurerm_container_group.test.fqdn
}

output "policy_definition_id" {
  value = azurerm_policy_definition.test.id
}

output "policy_assignment_id" {
  value = azurerm_subscription_policy_assignment.test.id
}

output "static_site_default_host_name" {
  value = azurerm_static_web_app.test.default_host_name
}

output "file_share_id" {
  value = azurerm_storage_share.test.id
}

output "notification_hub_id" {
  value = azurerm_notification_hub.test.id
}

output "signalr_hostname" {
  value = azurerm_signalr_service.test.hostname
}

output "postgresql_fqdn" {
  value = azurerm_postgresql_flexible_server.test.fqdn
}

output "postgresql_db_id" {
  value = azurerm_postgresql_flexible_server_database.test.id
}

output "mysql_fqdn" {
  value = azurerm_mysql_flexible_server.test.fqdn
}

output "recovery_vault_id" {
  value = azurerm_recovery_services_vault.test.id
}

output "redis_hostname" {
  value = azurerm_redis_cache.test.hostname
}

output "redis_primary_connection_string" {
  value     = azurerm_redis_cache.test.primary_connection_string
  sensitive = true
}
