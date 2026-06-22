# Smoke-test de Terraform contra el emulador.
#
# Nota: esto usa el provider genérico `http` (no el provider `azurerm`
# real) a propósito, para verificar que las rutas REST de cada fase
# responden con la forma esperada vía `local-exec`/`data "http"` — útil
# para CI/regresión de superficie amplia (todas las fases en un solo
# archivo), no para probar el ciclo de vida real de recursos azurerm.
#
# El provider `azurerm` real SÍ funciona contra este emulador desde la
# Fase 8 (descubrimiento de metadata ARM, emisor de tokens AAD falso,
# Microsoft Graph mínimo, TLS opcional, normalización de mayúsculas en
# rutas ARM) — ver terraform/azurerm-smoke-test/main.tf para ese caso, y
# "Testing with az CLI and Terraform" en README.md para los pasos
# completos (incluye confiar en el certificado autofirmado).

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

variable "location" {
  type    = string
  default = "eastus"
}

variable "vnet" {
  type    = string
  default = "tf-smoke-vnet"
}

variable "subnet" {
  type    = string
  default = "default"
}

variable "nic" {
  type    = string
  default = "tf-smoke-nic"
}

variable "disk" {
  type    = string
  default = "tf-smoke-disk"
}

variable "vm" {
  type    = string
  default = "tf-smoke-vm"
}

variable "vault" {
  type    = string
  default = "tfsmokekv"
}

variable "tenant_id" {
  type    = string
  default = "00000000-0000-0000-0000-000000000000"
}

variable "sb_namespace" {
  type    = string
  default = "tfsmokesbns"
}

variable "sb_queue" {
  type    = string
  default = "tf-smoke-sbqueue"
}

variable "cosmos_account" {
  type    = string
  default = "tfsmokecosmos"
}

variable "cosmos_db" {
  type    = string
  default = "tfsmokedb"
}

variable "cosmos_container" {
  type    = string
  default = "tfsmokecontainer"
}

variable "workspace" {
  type    = string
  default = "tfsmokeworkspace"
}

variable "action_group" {
  type    = string
  default = "tfsmokeactiongroup"
}

variable "metric_alert" {
  type    = string
  default = "tfsmokemetricalert"
}

variable "app_service_plan" {
  type    = string
  default = "tfsmokeplan"
}

variable "app_service_site" {
  type    = string
  default = "tfsmokesite"
}

variable "nsg" {
  type    = string
  default = "tfsmokensg"
}

variable "public_ip" {
  type    = string
  default = "tfsmokepip"
}

variable "load_balancer" {
  type    = string
  default = "tfsmokelb"
}

variable "route_table" {
  type    = string
  default = "tfsmokert"
}

variable "private_dns_zone" {
  type    = string
  default = "tfsmoke.internal"
}

variable "aks_cluster" {
  type    = string
  default = "tfsmokeaks"
}

variable "aks_node_pool" {
  type    = string
  default = "userpool"
}

variable "func_plan" {
  type    = string
  default = "tfsmokefuncplan"
}

variable "func_app" {
  type    = string
  default = "tfsmokefuncapp"
}

variable "func_name" {
  type    = string
  default = "HttpTrigger1"
}

variable "role_definition_id" {
  type    = string
  default = "tfsmoke11111111-1111-1111-1111-111111111111"
}

variable "role_assignment_name" {
  type    = string
  default = "tfsmoke22222222-2222-2222-2222-222222222222"
}

variable "role_assignment_rg_name" {
  type    = string
  default = "tfsmoke33333333-3333-3333-3333-333333333333"
}

variable "user_assigned_identity" {
  type    = string
  default = "tfsmokeidentity"
}

variable "eg_topic" {
  type    = string
  default = "tfsmoke-egtopic"
}

variable "eg_subscription" {
  type    = string
  default = "tfsmoke-egsub"
}

variable "eh_namespace" {
  type    = string
  default = "tfsmoketeh"
}

variable "eh_hub" {
  type    = string
  default = "tfsmoke-hub"
}

variable "eh_consumer_group" {
  type    = string
  default = "tfsmoke-cg"
}

variable "apim_service" {
  type    = string
  default = "tfsmokeapim"
}

variable "apim_api" {
  type    = string
  default = "echo"
}

variable "apim_operation" {
  type    = string
  default = "get-echo"
}

variable "apim_product" {
  type    = string
  default = "starter"
}

variable "apim_subscription" {
  type    = string
  default = "starter-sub"
}

# IDs de Microsoft.Network/Microsoft.Compute construidos a mano siguiendo el
# shape estándar de ARM: como el emulador no tiene un provider azurerm real
# (ver comentario al inicio del archivo), no hay un recurso de Terraform que
# nos dé esta cadena automáticamente como atributo — así que se construye
# igual que en scripts/test-az-cli.sh/.ps1 (SUBNET_ID/NIC_ID).
locals {
  subnet_id           = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}/subnets/${var.subnet}"
  nic_id              = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkInterfaces/${var.nic}"
  action_group_id     = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Insights/actionGroups/${var.action_group}"
  app_service_plan_id = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.app_service_plan}"
  public_ip_id        = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/publicIPAddresses/${var.public_ip}"
  load_balancer_id    = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/loadBalancers/${var.load_balancer}"
  func_plan_id        = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.func_plan}"
  role_definition_id  = "/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleDefinitions/${var.role_definition_id}"
  apim_product_id     = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/products/${var.apim_product}"
}

# PUT resource group (data source con `http` no soporta PUT, así que usamos
# un null_resource + local-exec para la escritura, y `http` solo para los
# GET de verificación).
#
# El local-exec usa PowerShell como intérprete en vez de depender de cmd/curl:
# en este equipo cmd /C no resuelve "curl"/"curl.exe" (PATH del proceso hijo
# no incluye System32 pese a estar en el PATH interactivo) y, aparte, cmd no
# soporta el escapado de comillas anidadas que necesita un body JSON, lo que
# rompía la URL para curl (exit code 3). Invoke-RestMethod evita ambos
# problemas.
resource "null_resource" "resource_group" {
  triggers = {
    rg = var.resource_group
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}?api-version=2021-04-01' -ContentType 'application/json' -Body '{\"location\": \"eastus\"}'"
  }
}

resource "null_resource" "storage_account" {
  depends_on = [null_resource.resource_group]
  triggers = {
    account = var.storage_account
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Storage/storageAccounts/${var.storage_account}?api-version=2023-01-01' -ContentType 'application/json' -Body '{\"location\": \"eastus\", \"sku\": {\"name\": \"Standard_LRS\"}, \"kind\": \"StorageV2\"}'"
  }
}

# Data plane: container + blob, montados bajo el endpoint path-style que
# devuelve storageaccounts.go (http://{endpoint}/{account}.blob/...). Igual
# que arriba, PUT se hace vía local-exec porque el provider `http` solo
# soporta GET; las verificaciones de lectura van con `data "http"`.
resource "null_resource" "blob_container" {
  depends_on = [null_resource.storage_account]
  triggers = {
    container = "tf-smoke-container"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.storage_account}.blob/tf-smoke-container?restype=container'"
  }
}

resource "null_resource" "blob" {
  depends_on = [null_resource.blob_container]
  triggers = {
    blob = "hello.txt"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.storage_account}.blob/tf-smoke-container/hello.txt' -ContentType 'text/plain' -Headers @{'x-ms-blob-type'='BlockBlob'} -Body 'hola mundo desde terraform'"
  }
}

# Data plane: queue + mensaje, montados bajo el endpoint path-style
# {account}.queue/... (mismo razonamiento que blob arriba).
resource "null_resource" "queue" {
  depends_on = [null_resource.storage_account]
  triggers = {
    queue = "tf-smoke-queue"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.storage_account}.queue/tf-smoke-queue'"
  }
}

resource "null_resource" "queue_message" {
  depends_on = [null_resource.queue]
  triggers = {
    message = "hola mundo desde terraform (queue)"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.storage_account}.queue/tf-smoke-queue/messages' -Body 'hola mundo desde terraform (queue)'"
  }
}

# Data plane: tabla + entidad, montadas bajo el endpoint path-style
# {account}.table/... (mismo razonamiento que blob/queue arriba). La
# entidad usa POST a la colección (sin parentesis en la URL) para no
# arrastrar el mismo problema de comillas/parentesis con local-exec que
# documentan los scripts de az CLI — Invoke-RestMethod no sufre el bug de
# az.cmd/cmd.exe, pero igual evitamos parentesis en la URL donde no son
# necesarios.
resource "null_resource" "table" {
  depends_on = [null_resource.storage_account]
  triggers = {
    table = "tfsmoketable"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.storage_account}.table/Tables' -ContentType 'application/json' -Body '{\"TableName\": \"tfsmoketable\"}'"
  }
}

resource "null_resource" "table_entity" {
  depends_on = [null_resource.table]
  triggers = {
    entity = "tf-smoke-entity"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.storage_account}.table/tfsmoketable' -ContentType 'application/json' -Body '{\"PartitionKey\": \"tf\", \"RowKey\": \"1\", \"Origin\": \"terraform\"}'"
  }
}

# Fase 4 (Compute/Network): mismo patrón null_resource + local-exec que el
# resto del archivo, ya que tampoco existe un provider azurerm real para
# estos recursos (ver comentario al inicio del archivo).
resource "null_resource" "vnet" {
  depends_on = [null_resource.resource_group]
  triggers = {
    vnet = var.vnet
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"properties\": {\"addressSpace\": {\"addressPrefixes\": [\"10.0.0.0/16\"]}}}'"
  }
}

resource "null_resource" "subnet" {
  depends_on = [null_resource.vnet]
  triggers = {
    subnet = var.subnet
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}/subnets/${var.subnet}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"properties\": {\"addressPrefix\": \"10.0.1.0/24\"}}'"
  }
}

resource "null_resource" "nic" {
  depends_on = [null_resource.subnet]
  triggers = {
    nic = var.nic
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkInterfaces/${var.nic}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"properties\": {\"ipConfigurations\": [{\"name\": \"ipconfig1\", \"properties\": {\"subnet\": {\"id\": \"${local.subnet_id}\"}}}]}}'"
  }
}

resource "null_resource" "disk" {
  depends_on = [null_resource.resource_group]
  triggers = {
    disk = var.disk
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Compute/disks/${var.disk}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"sku\": {\"name\": \"Standard_LRS\"}, \"properties\": {\"diskSizeGB\": 32, \"creationData\": {\"createOption\": \"Empty\"}}}'"
  }
}

resource "null_resource" "vm" {
  depends_on = [null_resource.nic]
  triggers = {
    vm = var.vm
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Compute/virtualMachines/${var.vm}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"properties\": {\"hardwareProfile\": {\"vmSize\": \"Standard_B1s\"}, \"storageProfile\": {\"imageReference\": {\"publisher\": \"Canonical\", \"offer\": \"0001-com-ubuntu-server-jammy\", \"sku\": \"22_04-lts-gen2\", \"version\": \"latest\"}}, \"osProfile\": {\"computerName\": \"tfsmokevm\", \"adminUsername\": \"azureuser\", \"adminPassword\": \"P@ssw0rd1234!\"}, \"networkProfile\": {\"networkInterfaces\": [{\"id\": \"${local.nic_id}\"}]}}}'"
  }
}

# Fase 5 (Key Vault): vault (ARM, síncrono) + secret/key/certificate
# (data plane bajo {vault}.vault/...), mismo patrón null_resource +
# local-exec del resto del archivo.
resource "null_resource" "vault" {
  depends_on = [null_resource.resource_group]
  triggers = {
    vault = var.vault
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.KeyVault/vaults/${var.vault}?api-version=2023-07-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"properties\": {\"sku\": {\"family\": \"A\", \"name\": \"standard\"}, \"tenantId\": \"${var.tenant_id}\"}}'"
  }
}

resource "null_resource" "secret" {
  depends_on = [null_resource.vault]
  triggers = {
    secret = "tf-smoke-secret"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.vault}.vault/secrets/tf-smoke-secret' -ContentType 'application/json' -Body '{\"value\": \"super-secreto-terraform\"}'"
  }
}

resource "null_resource" "key" {
  depends_on = [null_resource.vault]
  triggers = {
    key = "tf-smoke-key"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.vault}.vault/keys/tf-smoke-key' -ContentType 'application/json' -Body '{\"kty\": \"RSA\"}'"
  }
}

resource "null_resource" "certificate" {
  depends_on = [null_resource.vault]
  triggers = {
    certificate = "tf-smoke-cert"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.vault}.vault/certificates/tf-smoke-cert' -ContentType 'application/json' -Body '{\"policy\": {}}'"
  }
}

# Fase 6 (Service Bus): namespace (ARM, asíncrono) + queue (ARM, síncrono)
# + mensaje (data plane bajo {namespace}.servicebus/...), mismo patrón
# null_resource + local-exec del resto del archivo.
resource "null_resource" "sb_namespace" {
  depends_on = [null_resource.resource_group]
  triggers = {
    namespace = var.sb_namespace
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ServiceBus/namespaces/${var.sb_namespace}?api-version=2021-11-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "sb_queue" {
  depends_on = [null_resource.sb_namespace]
  triggers = {
    queue = var.sb_queue
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ServiceBus/namespaces/${var.sb_namespace}/queues/${var.sb_queue}?api-version=2021-11-01' -ContentType 'application/json' -Body '{\"properties\": {}}'"
  }
}

resource "null_resource" "sb_message" {
  depends_on = [null_resource.sb_queue]
  triggers = {
    message = "hola mundo desde terraform (service bus)"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.sb_namespace}.servicebus/${var.sb_queue}/messages' -ContentType 'application/json' -Body '{\"body\": \"hola mundo desde terraform (service bus)\"}'"
  }
}

# Fase 6 (Cosmos DB): account (ARM, asíncrono) + sqlDatabase + container
# (ARM, síncronos) + documento (data plane bajo {account}.documents/...),
# mismo patrón null_resource + local-exec del resto del archivo.
resource "null_resource" "cosmos_account" {
  depends_on = [null_resource.resource_group]
  triggers = {
    account = var.cosmos_account
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.DocumentDB/databaseAccounts/${var.cosmos_account}?api-version=2023-04-15' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "cosmos_db" {
  depends_on = [null_resource.cosmos_account]
  triggers = {
    db = var.cosmos_db
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.DocumentDB/databaseAccounts/${var.cosmos_account}/sqlDatabases/${var.cosmos_db}?api-version=2023-04-15' -ContentType 'application/json' -Body '{\"properties\": {\"resource\": {\"id\": \"${var.cosmos_db}\"}}}'"
  }
}

resource "null_resource" "cosmos_container" {
  depends_on = [null_resource.cosmos_db]
  triggers = {
    container = var.cosmos_container
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.DocumentDB/databaseAccounts/${var.cosmos_account}/sqlDatabases/${var.cosmos_db}/containers/${var.cosmos_container}?api-version=2023-04-15' -ContentType 'application/json' -Body '{\"properties\": {\"resource\": {\"id\": \"${var.cosmos_container}\", \"partitionKey\": {\"paths\": [\"/pk\"]}}}}'"
  }
}

resource "null_resource" "cosmos_document" {
  depends_on = [null_resource.cosmos_container]
  triggers = {
    doc = "tf-smoke-doc"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/${var.cosmos_account}.documents/dbs/${var.cosmos_db}/colls/${var.cosmos_container}/docs/tf-smoke-doc' -ContentType 'application/json' -Body '{\"pk\": \"x\", \"origin\": \"terraform\"}'"
  }
}

# Fase 10 (Monitor + Log Analytics): workspace (ARM, síncrono) + action
# group (ARM, síncrono) + metric alert (ARM, síncrono, referencia el action
# group por id), mismo patrón null_resource + local-exec del resto del
# archivo. No hay equivalente de data plane real que verificar más allá del
# stub de Log Analytics Query (ver dataplane.go) -- queda fuera de este
# smoke test porque siempre devuelve una tabla vacía sin importar el input.
resource "null_resource" "workspace" {
  depends_on = [null_resource.resource_group]
  triggers = {
    workspace = var.workspace
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.OperationalInsights/workspaces/${var.workspace}?api-version=2022-10-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "action_group" {
  depends_on = [null_resource.resource_group]
  triggers = {
    action_group = var.action_group
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Insights/actionGroups/${var.action_group}?api-version=2021-08-01' -ContentType 'application/json' -Body '{\"location\": \"global\", \"properties\": {\"groupShortName\": \"tfsmoke\", \"emailReceivers\": [{\"name\": \"admin\", \"emailAddress\": \"admin@example.com\"}]}}'"
  }
}

resource "null_resource" "metric_alert" {
  depends_on = [null_resource.action_group, null_resource.nic]
  triggers = {
    metric_alert = var.metric_alert
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Insights/metricAlerts/${var.metric_alert}?api-version=2021-08-01' -ContentType 'application/json' -Body '{\"location\": \"global\", \"properties\": {\"severity\": 2, \"scopes\": [\"${local.nic_id}\"], \"criteria\": {\"odata.type\": \"Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria\"}, \"actions\": [{\"actionGroupId\": \"${local.action_group_id}\"}]}}'"
  }
}

# Fase 11 (App Service): plan (ARM, síncrono) + web app (ARM, síncrono,
# referencia el plan por id, igual que metric_alert referencia el action
# group) + app settings (StringDictionary, sub-recurso síncrono), mismo
# patrón null_resource + local-exec del resto del archivo.
resource "null_resource" "app_service_plan" {
  depends_on = [null_resource.resource_group]
  triggers = {
    plan = var.app_service_plan
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.app_service_plan}?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"kind\": \"linux\", \"sku\": {\"name\": \"B1\", \"tier\": \"Basic\"}}'"
  }
}

resource "null_resource" "app_service_site" {
  depends_on = [null_resource.app_service_plan]
  triggers = {
    site = var.app_service_site
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.app_service_site}?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"kind\": \"app,linux\", \"properties\": {\"serverFarmId\": \"${local.app_service_plan_id}\", \"siteConfig\": {\"linuxFxVersion\": \"DOCKER|nginx:latest\"}}}'"
  }
}

resource "null_resource" "app_service_settings" {
  depends_on = [null_resource.app_service_site]
  triggers = {
    settings = "tf-smoke-appsettings"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.app_service_site}/config/appsettings?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"properties\": {\"WEBSITES_PORT\": \"8080\"}}'"
  }
}

# Fase 12 (Networking): NSG + regla, Public IP, Load Balancer (referencia
# la public IP por id), Route Table + ruta, y Private DNS zone + registro A.
# Mismo patrón null_resource + local-exec del resto del archivo.
resource "null_resource" "nsg" {
  depends_on = [null_resource.resource_group]
  triggers = {
    nsg = var.nsg
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkSecurityGroups/${var.nsg}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "nsg_rule" {
  depends_on = [null_resource.nsg]
  triggers = {
    rule = "allow-ssh"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkSecurityGroups/${var.nsg}/securityRules/allow-ssh?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"properties\": {\"priority\": 100, \"direction\": \"Inbound\", \"access\": \"Allow\", \"protocol\": \"Tcp\", \"sourceAddressPrefix\": \"*\", \"destinationAddressPrefix\": \"*\", \"sourcePortRange\": \"*\", \"destinationPortRange\": \"22\"}}'"
  }
}

resource "null_resource" "public_ip" {
  depends_on = [null_resource.resource_group]
  triggers = {
    pip = var.public_ip
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/publicIPAddresses/${var.public_ip}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "load_balancer" {
  depends_on = [null_resource.public_ip]
  triggers = {
    lb = var.load_balancer
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/loadBalancers/${var.load_balancer}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"properties\": {\"frontendIPConfigurations\": [{\"name\": \"frontend1\", \"properties\": {\"publicIPAddress\": {\"id\": \"${local.public_ip_id}\"}}}], \"backendAddressPools\": [{\"name\": \"backend1\"}], \"loadBalancingRules\": [{\"name\": \"rule1\", \"properties\": {\"frontendIPConfiguration\": {\"id\": \"${local.load_balancer_id}/frontendIPConfigurations/frontend1\"}, \"backendAddressPool\": {\"id\": \"${local.load_balancer_id}/backendAddressPools/backend1\"}, \"protocol\": \"Tcp\", \"frontendPort\": 80, \"backendPort\": 8080}}]}}'"
  }
}

resource "null_resource" "route_table" {
  depends_on = [null_resource.resource_group]
  triggers = {
    rt = var.route_table
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/routeTables/${var.route_table}?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "route" {
  depends_on = [null_resource.route_table]
  triggers = {
    route = "to-internet"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/routeTables/${var.route_table}/routes/to-internet?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"properties\": {\"addressPrefix\": \"0.0.0.0/0\", \"nextHopType\": \"Internet\"}}'"
  }
}

# location se fuerza a "global" server-side (ver internal/services/network),
# así que el body de creación va vacío, igual que en test-az-cli.ps1/.sh.
resource "null_resource" "private_dns_zone" {
  depends_on = [null_resource.resource_group]
  triggers = {
    zone = var.private_dns_zone
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/privateDnsZones/${var.private_dns_zone}?api-version=2023-09-01' -ContentType 'application/json' -Body '{}'"
  }
}

resource "null_resource" "private_dns_record" {
  depends_on = [null_resource.private_dns_zone]
  triggers = {
    record = "www"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/privateDnsZones/${var.private_dns_zone}/A/www?api-version=2023-09-01' -ContentType 'application/json' -Body '{\"properties\": {\"ttl\": 300, \"aRecords\": [{\"ipv4Address\": \"10.0.0.4\"}]}}'"
  }
}

# Fase 13 (AKS): managed cluster (ARM, asíncrono, "shape-compatible, not
# behavior-complete" — no hay un control plane de Kubernetes real detrás)
# + agent pool (sub-recurso, también asíncrono). Mismo patrón null_resource
# + local-exec del resto del archivo.
resource "null_resource" "aks_cluster" {
  depends_on = [null_resource.resource_group]
  triggers = {
    cluster = var.aks_cluster
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ContainerService/managedClusters/${var.aks_cluster}?api-version=2023-10-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"identity\": {\"type\": \"SystemAssigned\"}, \"properties\": {\"dnsPrefix\": \"${var.aks_cluster}\"}}'"
  }
}

resource "null_resource" "aks_node_pool" {
  depends_on = [null_resource.aks_cluster]
  triggers = {
    pool = var.aks_node_pool
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ContainerService/managedClusters/${var.aks_cluster}/agentPools/${var.aks_node_pool}?api-version=2023-10-01' -ContentType 'application/json' -Body '{\"properties\": {\"vmSize\": \"Standard_DS2_v2\", \"count\": 2, \"mode\": \"User\"}}'"
  }
}

# Fase 14 (Functions): un Function App ES un Microsoft.Web/sites con
# kind="functionapp,linux" — ya soportado por appservice.putSite sin
# cambios (ver internal/services/functions/functiondefs.go). El plan usa
# SKU Y1/Dynamic (consumption plan real de Azure Functions). El sub-recurso
# Microsoft.Web/sites/functions/{name} es síncrono y no valida que el site
# padre exista (mismo "sin integridad referencial estricta" que
# metric_alert/action_group arriba). Mismo patrón null_resource + local-exec
# del resto del archivo.
resource "null_resource" "func_plan" {
  depends_on = [null_resource.resource_group]
  triggers = {
    plan = var.func_plan
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.func_plan}?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"kind\": \"functionapp\", \"sku\": {\"name\": \"Y1\", \"tier\": \"Dynamic\"}}'"
  }
}

resource "null_resource" "func_app" {
  depends_on = [null_resource.func_plan]
  triggers = {
    app = var.func_app
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.func_app}?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"kind\": \"functionapp,linux\", \"properties\": {\"serverFarmId\": \"${local.func_plan_id}\"}}'"
  }
}

resource "null_resource" "func_definition" {
  depends_on = [null_resource.func_app]
  triggers = {
    name = var.func_name
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.func_app}/functions/${var.func_name}?api-version=2022-03-01' -ContentType 'application/json' -Body '{\"properties\": {\"language\": \"python\", \"config\": {\"bindings\": [{\"type\": \"httpTrigger\", \"direction\": \"in\", \"authLevel\": \"function\"}]}}}'"
  }
}

# syncfunctiontriggers es una acción sync sin cuerpo (204) — se invoca vía
# null_resource/local-exec porque no es una lectura idempotente (el
# provider `http` solo hace GET).
resource "null_resource" "func_sync_triggers" {
  depends_on = [null_resource.func_definition]
  triggers = {
    always_run = timestamp()
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.func_app}/syncfunctiontriggers?api-version=2022-03-01'"
  }
}

# Fase 15 (Entra ID + RBAC): role definition (ARM, síncrono, scope de
# suscripción) + role assignment a nivel de suscripción y a nivel de
# resource group (dos buckets separados server-side — ver
# internal/services/authorization/roleassignments.go — para que el listado
# a nivel de suscripción nunca incluya una asignación de un resource group
# cuyo subscriptionId coincide). Mismo patrón null_resource + local-exec del
# resto del archivo, ya que no existe un provider azurerm/azuread real
# detrás de este emulador (ver comentario al inicio del archivo).
resource "null_resource" "role_definition" {
  depends_on = [null_resource.resource_group]
  triggers = {
    role_definition = var.role_definition_id
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleDefinitions/${var.role_definition_id}?api-version=2022-04-01' -ContentType 'application/json' -Body '{\"properties\": {\"roleName\": \"tf-smoke-custom-role\", \"description\": \"rol de prueba creado por terraform smoke-test\", \"assignableScopes\": [\"/subscriptions/${var.subscription_id}\"], \"permissions\": [{\"actions\": [\"Microsoft.Storage/storageAccounts/read\"]}]}}'"
  }
}

resource "null_resource" "role_assignment" {
  depends_on = [null_resource.role_definition]
  triggers = {
    assignment = var.role_assignment_name
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleAssignments/${var.role_assignment_name}?api-version=2022-04-01' -ContentType 'application/json' -Body '{\"properties\": {\"roleDefinitionId\": \"${local.role_definition_id}\", \"principalId\": \"tf-smoke-principal\"}}'"
  }
}

resource "null_resource" "role_assignment_rg" {
  depends_on = [null_resource.role_definition]
  triggers = {
    assignment = var.role_assignment_rg_name
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Authorization/roleAssignments/${var.role_assignment_rg_name}?api-version=2022-04-01' -ContentType 'application/json' -Body '{\"properties\": {\"roleDefinitionId\": \"${local.role_definition_id}\", \"principalId\": \"tf-smoke-principal-rg\"}}'"
  }
}

# Fase 16 (Managed Identities): user-assigned identity (ARM, síncrono) --
# mismo patrón null_resource + local-exec del resto del archivo. El emulador
# deriva tenantId/principalId/clientId de forma determinista (ver
# internal/services/managedidentity/identities.go), así que no hace falta
# capturar nada de la respuesta del PUT para usarlo en otro recurso de este
# archivo.
resource "null_resource" "user_assigned_identity" {
  depends_on = [null_resource.resource_group]
  triggers = {
    identity = var.user_assigned_identity
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/${var.user_assigned_identity}?api-version=2023-01-31' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

# Fase 17 (Eventing): Event Grid topic + event subscription (webhook) +
# publish, y Event Hubs namespace (async) + event hub + consumer group +
# send (data-plane). El subscriber del webhook es un placeholder
# (localhost:10999) porque solo nos interesa verificar que el emulador
# acepta y persiste la suscripción y que el publish dispara el intento de
# entrega (lastDeliveryStatus se verifica en el bloque data "http" más abajo).
resource "null_resource" "eg_topic" {
  depends_on = [null_resource.resource_group]
  triggers = {
    topic = var.eg_topic
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventGrid/topics/${var.eg_topic}?api-version=2022-06-15' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\"}'"
  }
}

resource "null_resource" "eg_subscription" {
  depends_on = [null_resource.eg_topic]
  triggers = {
    sub = var.eg_subscription
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventGrid/topics/${var.eg_topic}/providers/Microsoft.EventGrid/eventSubscriptions/${var.eg_subscription}?api-version=2022-06-15' -ContentType 'application/json' -Body '{\"properties\": {\"destination\": {\"endpointType\": \"WebHook\", \"properties\": {\"endpointUrl\": \"http://localhost:10999/webhook\"}}}}'"
  }
}

resource "null_resource" "eg_publish" {
  depends_on = [null_resource.eg_subscription]
  triggers = {
    topic = var.eg_topic
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.eg_topic}.eventgrid/api/events' -ContentType 'application/json' -Body '[{\"id\": \"tfsmoke-event-1\", \"eventType\": \"TfSmoke.Test\", \"subject\": \"tfsmoke\", \"eventTime\": \"2024-01-01T00:00:00Z\", \"data\": {\"hello\": \"terraform\"}, \"dataVersion\": \"1.0\"}]'; Start-Sleep -Seconds 1"
  }
}

resource "null_resource" "eh_namespace" {
  depends_on = [null_resource.resource_group]
  triggers = {
    ns = var.eh_namespace
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}?api-version=2021-11-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"sku\": {\"name\": \"Standard\"}}'"
  }
}

resource "null_resource" "eh_hub" {
  depends_on = [null_resource.eh_namespace]
  triggers = {
    hub = var.eh_hub
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}/eventhubs/${var.eh_hub}?api-version=2021-11-01' -ContentType 'application/json' -Body '{}'"
  }
}

resource "null_resource" "eh_consumer_group" {
  depends_on = [null_resource.eh_hub]
  triggers = {
    cg = var.eh_consumer_group
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}/eventhubs/${var.eh_hub}/consumergroups/${var.eh_consumer_group}?api-version=2021-11-01' -ContentType 'application/json' -Body '{}'"
  }
}

resource "null_resource" "eh_send" {
  depends_on = [null_resource.eh_consumer_group]
  triggers = {
    hub = var.eh_hub
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Post -Uri '${var.endpoint}/${var.eh_namespace}.eventhub/${var.eh_hub}/messages' -ContentType 'text/plain' -Body 'hola desde terraform smoke test'"
  }
}

# Fase 18 (API Management): service instance (ARM, asíncrono, mismo patrón
# que AKS/managedClusters) + API + operation (sub-recursos síncronos) +
# product + asociación product-api (síncronos) + subscription (síncrono,
# primaryKey/secondaryKey deterministas vía fakeGUID — ver
# internal/services/apimanagement/service.go). Mismo patrón null_resource +
# local-exec del resto del archivo.
resource "null_resource" "apim_service" {
  depends_on = [null_resource.resource_group]
  triggers = {
    service = var.apim_service
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}?api-version=2022-08-01' -ContentType 'application/json' -Body '{\"location\": \"${var.location}\", \"sku\": {\"name\": \"Developer\", \"capacity\": 1}, \"properties\": {\"publisherEmail\": \"admin@example.com\", \"publisherName\": \"Contoso\"}}'"
  }
}

resource "null_resource" "apim_api" {
  depends_on = [null_resource.apim_service]
  triggers = {
    api = var.apim_api
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/apis/${var.apim_api}?api-version=2022-08-01' -ContentType 'application/json' -Body '{\"properties\": {\"displayName\": \"Echo API\", \"path\": \"echo\", \"serviceUrl\": \"https://backend.example.com\"}}'"
  }
}

resource "null_resource" "apim_operation" {
  depends_on = [null_resource.apim_api]
  triggers = {
    operation = var.apim_operation
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/apis/${var.apim_api}/operations/${var.apim_operation}?api-version=2022-08-01' -ContentType 'application/json' -Body '{\"properties\": {\"displayName\": \"GET echo\", \"method\": \"GET\", \"urlTemplate\": \"/{id}\"}}'"
  }
}

resource "null_resource" "apim_product" {
  depends_on = [null_resource.apim_service]
  triggers = {
    product = var.apim_product
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/products/${var.apim_product}?api-version=2022-08-01' -ContentType 'application/json' -Body '{\"properties\": {\"displayName\": \"Starter\"}}'"
  }
}

resource "null_resource" "apim_product_api" {
  depends_on = [null_resource.apim_product, null_resource.apim_api]
  triggers = {
    association = "${var.apim_product}-${var.apim_api}"
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/products/${var.apim_product}/apis/${var.apim_api}?api-version=2022-08-01'"
  }
}

resource "null_resource" "apim_subscription" {
  depends_on = [null_resource.apim_product_api]
  triggers = {
    subscription = var.apim_subscription
  }

  provisioner "local-exec" {
    interpreter = ["PowerShell", "-Command"]
    command     = "Invoke-RestMethod -Method Put -Uri '${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/subscriptions/${var.apim_subscription}?api-version=2022-08-01' -ContentType 'application/json' -Body '{\"properties\": {\"displayName\": \"Starter subscription\", \"scope\": \"${local.apim_product_id}\"}}'"
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

data "http" "blob_list" {
  depends_on = [null_resource.blob]
  url        = "${var.endpoint}/${var.storage_account}.blob/tf-smoke-container?restype=container&comp=list"
}

data "http" "blob_content" {
  depends_on = [null_resource.blob]
  url        = "${var.endpoint}/${var.storage_account}.blob/tf-smoke-container/hello.txt"
}

# peekonly=true para no consumir/reservar el mensaje en cada refresh de
# Terraform (a diferencia de un GET de dequeue normal, peek no cambia
# dequeueCount ni oculta el mensaje, así que es seguro para una data
# source que Terraform puede releer en cualquier momento).
data "http" "queue_metadata" {
  depends_on = [null_resource.queue]
  url        = "${var.endpoint}/${var.storage_account}.queue/tf-smoke-queue?comp=metadata"
}

data "http" "queue_message_peek" {
  depends_on = [null_resource.queue_message]
  url        = "${var.endpoint}/${var.storage_account}.queue/tf-smoke-queue/messages?peekonly=true"
}

data "http" "table_list" {
  depends_on = [null_resource.table]
  url        = "${var.endpoint}/${var.storage_account}.table/Tables"
}

# El provider http hace el GET directamente (no pasa por cmd.exe), así que
# los parentesis/comillas simples de la dirección de entidad puntual no
# tienen el mismo problema documentado para az.cmd en los scripts de az CLI.
data "http" "table_entity" {
  depends_on = [null_resource.table_entity]
  url        = "${var.endpoint}/${var.storage_account}.table/tfsmoketable(PartitionKey='tf',RowKey='1')"
}

# Fase 4 (Compute/Network): verificación de lectura, mismo patrón.
data "http" "vnet" {
  depends_on = [null_resource.vnet]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}?api-version=2023-09-01"
}

data "http" "subnet" {
  depends_on = [null_resource.subnet]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}/subnets/${var.subnet}?api-version=2023-09-01"
}

data "http" "nic" {
  depends_on = [null_resource.nic]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkInterfaces/${var.nic}?api-version=2023-09-01"
}

data "http" "disk" {
  depends_on = [null_resource.disk]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Compute/disks/${var.disk}?api-version=2023-09-01"
}

# La respuesta de la VM no debe incluir adminPassword (igual que en los
# scripts de az CLI): ver internal/services/compute/vms.go, OsProfile no
# tiene ese campo en el struct de salida.
data "http" "vm" {
  depends_on = [null_resource.vm]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Compute/virtualMachines/${var.vm}?api-version=2023-09-01"
}

data "http" "vault" {
  depends_on = [null_resource.vault]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.KeyVault/vaults/${var.vault}?api-version=2023-07-01"
}

# El listado de secrets nunca incluye "value" (igual que la API real), así
# que esta data source es segura para releer en cualquier momento sin
# preocuparse por exponer el secreto en el state de Terraform.
data "http" "secret_list" {
  depends_on = [null_resource.secret]
  url        = "${var.endpoint}/${var.vault}.vault/secrets"
}

data "http" "key" {
  depends_on = [null_resource.key]
  url        = "${var.endpoint}/${var.vault}.vault/keys/tf-smoke-key"
}

data "http" "certificate" {
  depends_on = [null_resource.certificate]
  url        = "${var.endpoint}/${var.vault}.vault/certificates/tf-smoke-cert"
}

data "http" "sb_namespace" {
  depends_on = [null_resource.sb_namespace]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ServiceBus/namespaces/${var.sb_namespace}?api-version=2021-11-01"
}

# peeklock=true reserva el mensaje (no es idempotente como el peekonly de
# queue storage), pero al ser una data source solo se evalúa una vez por
# `terraform apply`, igual que el resto de las data sources de este archivo.
data "http" "sb_message_peek" {
  depends_on = [null_resource.sb_message]
  url        = "${var.endpoint}/${var.sb_namespace}.servicebus/${var.sb_queue}/messages?peeklock=true"
}

data "http" "cosmos_account" {
  depends_on = [null_resource.cosmos_account]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.DocumentDB/databaseAccounts/${var.cosmos_account}?api-version=2023-04-15"
}

data "http" "cosmos_container" {
  depends_on = [null_resource.cosmos_container]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.DocumentDB/databaseAccounts/${var.cosmos_account}/sqlDatabases/${var.cosmos_db}/containers/${var.cosmos_container}?api-version=2023-04-15"
}

data "http" "cosmos_document" {
  depends_on = [null_resource.cosmos_document]
  url        = "${var.endpoint}/${var.cosmos_account}.documents/dbs/${var.cosmos_db}/colls/${var.cosmos_container}/docs/tf-smoke-doc"
}

data "http" "workspace" {
  depends_on = [null_resource.workspace]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.OperationalInsights/workspaces/${var.workspace}?api-version=2022-10-01"
}

data "http" "action_group" {
  depends_on = [null_resource.action_group]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Insights/actionGroups/${var.action_group}?api-version=2021-08-01"
}

data "http" "metric_alert" {
  depends_on = [null_resource.metric_alert]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Insights/metricAlerts/${var.metric_alert}?api-version=2021-08-01"
}

data "http" "app_service_plan" {
  depends_on = [null_resource.app_service_plan]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.app_service_plan}?api-version=2022-03-01"
}

data "http" "app_service_site" {
  depends_on = [null_resource.app_service_site]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.app_service_site}?api-version=2022-03-01"
}

data "http" "app_service_settings" {
  depends_on = [null_resource.app_service_settings]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.app_service_site}/config/appsettings?api-version=2022-03-01"
}

# Fase 12 (Networking): verificación de lectura, mismo patrón.
data "http" "nsg" {
  depends_on = [null_resource.nsg_rule]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkSecurityGroups/${var.nsg}?api-version=2023-09-01"
}

data "http" "public_ip" {
  depends_on = [null_resource.public_ip]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/publicIPAddresses/${var.public_ip}?api-version=2023-09-01"
}

data "http" "load_balancer" {
  depends_on = [null_resource.load_balancer]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/loadBalancers/${var.load_balancer}?api-version=2023-09-01"
}

data "http" "route_table" {
  depends_on = [null_resource.route]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/routeTables/${var.route_table}?api-version=2023-09-01"
}

data "http" "private_dns_zone" {
  depends_on = [null_resource.private_dns_record]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/privateDnsZones/${var.private_dns_zone}?api-version=2023-09-01"
}

# Fase 13 (AKS): verificación de lectura, mismo patrón.
data "http" "aks_cluster" {
  depends_on = [null_resource.aks_node_pool]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ContainerService/managedClusters/${var.aks_cluster}?api-version=2023-10-01"
}

data "http" "aks_node_pool" {
  depends_on = [null_resource.aks_node_pool]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ContainerService/managedClusters/${var.aks_cluster}/agentPools/${var.aks_node_pool}?api-version=2023-10-01"
}

# Fase 14 (Functions): verificación de lectura, mismo patrón. listkeys no
# se modela como data source porque es un POST (no GET) — su verificación
# vive en scripts/test-az-cli.ps1/.sh, no aquí.
data "http" "func_plan" {
  depends_on = [null_resource.func_plan]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/serverfarms/${var.func_plan}?api-version=2022-03-01"
}

data "http" "func_app" {
  depends_on = [null_resource.func_app]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.func_app}?api-version=2022-03-01"
}

data "http" "func_definition" {
  depends_on = [null_resource.func_sync_triggers]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Web/sites/${var.func_app}/functions/${var.func_name}?api-version=2022-03-01"
}

# Fase 15 (Entra ID + RBAC): verificación de lectura, mismo patrón.
data "http" "role_definition" {
  depends_on = [null_resource.role_definition]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleDefinitions/${var.role_definition_id}?api-version=2022-04-01"
}

data "http" "role_assignment" {
  depends_on = [null_resource.role_assignment]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleAssignments/${var.role_assignment_name}?api-version=2022-04-01"
}

data "http" "role_assignment_rg" {
  depends_on = [null_resource.role_assignment_rg]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Authorization/roleAssignments/${var.role_assignment_rg_name}?api-version=2022-04-01"
}

# La lista a nivel de suscripción no debe incluir la asignación de resource
# group (ver TestRoleAssignmentResourceGroupScope en
# internal/services/authorization/authorization_test.go para el mismo check
# vía Go test) -- esta data source es la verificación equivalente desde el
# lado de Terraform.
data "http" "role_assignments_sub_list" {
  depends_on = [null_resource.role_assignment, null_resource.role_assignment_rg]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/providers/Microsoft.Authorization/roleAssignments?api-version=2022-04-01"
}

# Fase 16 (Managed Identities): verificación de lectura, mismo patrón.
data "http" "user_assigned_identity" {
  depends_on = [null_resource.user_assigned_identity]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/${var.user_assigned_identity}?api-version=2023-01-31"
}

data "http" "eg_topic" {
  depends_on = [null_resource.eg_topic]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventGrid/topics/${var.eg_topic}?api-version=2022-06-15"
}

data "http" "eg_subscription" {
  depends_on = [null_resource.eg_publish]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventGrid/topics/${var.eg_topic}/providers/Microsoft.EventGrid/eventSubscriptions/${var.eg_subscription}?api-version=2022-06-15"
}

data "http" "eh_namespace" {
  depends_on = [null_resource.eh_namespace]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}?api-version=2021-11-01"
}

data "http" "eh_hub" {
  depends_on = [null_resource.eh_hub]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}/eventhubs/${var.eh_hub}?api-version=2021-11-01"
}

data "http" "eh_consumer_group" {
  depends_on = [null_resource.eh_consumer_group]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.EventHub/namespaces/${var.eh_namespace}/eventhubs/${var.eh_hub}/consumergroups/${var.eh_consumer_group}?api-version=2021-11-01"
}

data "http" "eh_receive" {
  depends_on = [null_resource.eh_send]
  url        = "${var.endpoint}/${var.eh_namespace}.eventhub/${var.eh_hub}/messages"
}

# Fase 18 (API Management): verificación de lectura, mismo patrón.
data "http" "apim_service" {
  depends_on = [null_resource.apim_service]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}?api-version=2022-08-01"
}

data "http" "apim_api" {
  depends_on = [null_resource.apim_api]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/apis/${var.apim_api}?api-version=2022-08-01"
}

data "http" "apim_operation" {
  depends_on = [null_resource.apim_operation]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/apis/${var.apim_api}/operations/${var.apim_operation}?api-version=2022-08-01"
}

data "http" "apim_product" {
  depends_on = [null_resource.apim_product_api]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/products/${var.apim_product}?api-version=2022-08-01"
}

# Las claves devueltas aquí deben coincidir con las verificadas vía az CLI
# (mismo seed determinista, ver fakeGUID en
# internal/services/apimanagement/service.go) -- útil para confirmar que el
# valor no cambia entre runs/herramientas distintas contra una misma DB.
data "http" "apim_subscription" {
  depends_on = [null_resource.apim_subscription]
  url        = "${var.endpoint}/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.ApiManagement/service/${var.apim_service}/subscriptions/${var.apim_subscription}?api-version=2022-08-01"
}

output "resource_group_response" {
  value = jsondecode(data.http.resource_group.response_body)
}

output "storage_account_response" {
  value = jsondecode(data.http.storage_account.response_body)
}

output "blob_list_response" {
  value = jsondecode(data.http.blob_list.response_body)
}

output "blob_content" {
  value = data.http.blob_content.response_body
}

output "queue_metadata_response" {
  value = jsondecode(data.http.queue_metadata.response_body)
}

output "queue_message_peek_response" {
  value = jsondecode(data.http.queue_message_peek.response_body)
}

output "table_list_response" {
  value = jsondecode(data.http.table_list.response_body)
}

output "table_entity_response" {
  value = jsondecode(data.http.table_entity.response_body)
}

output "vnet_response" {
  value = jsondecode(data.http.vnet.response_body)
}

output "subnet_response" {
  value = jsondecode(data.http.subnet.response_body)
}

output "nic_response" {
  value = jsondecode(data.http.nic.response_body)
}

output "disk_response" {
  value = jsondecode(data.http.disk.response_body)
}

output "vm_response" {
  value = jsondecode(data.http.vm.response_body)
}

output "vault_response" {
  value = jsondecode(data.http.vault.response_body)
}

output "secret_list_response" {
  value = jsondecode(data.http.secret_list.response_body)
}

output "key_response" {
  value = jsondecode(data.http.key.response_body)
}

output "certificate_response" {
  value = jsondecode(data.http.certificate.response_body)
}

output "sb_namespace_response" {
  value = jsondecode(data.http.sb_namespace.response_body)
}

output "sb_message_peek_response" {
  value = jsondecode(data.http.sb_message_peek.response_body)
}

output "cosmos_account_response" {
  value = jsondecode(data.http.cosmos_account.response_body)
}

output "cosmos_container_response" {
  value = jsondecode(data.http.cosmos_container.response_body)
}

output "cosmos_document_response" {
  value = jsondecode(data.http.cosmos_document.response_body)
}

output "workspace_response" {
  value = jsondecode(data.http.workspace.response_body)
}

output "action_group_response" {
  value = jsondecode(data.http.action_group.response_body)
}

output "metric_alert_response" {
  value = jsondecode(data.http.metric_alert.response_body)
}

output "app_service_plan_response" {
  value = jsondecode(data.http.app_service_plan.response_body)
}

output "app_service_site_response" {
  value = jsondecode(data.http.app_service_site.response_body)
}

output "app_service_settings_response" {
  value = jsondecode(data.http.app_service_settings.response_body)
}

output "nsg_response" {
  value = jsondecode(data.http.nsg.response_body)
}

output "public_ip_response" {
  value = jsondecode(data.http.public_ip.response_body)
}

output "load_balancer_response" {
  value = jsondecode(data.http.load_balancer.response_body)
}

output "route_table_response" {
  value = jsondecode(data.http.route_table.response_body)
}

output "private_dns_zone_response" {
  value = jsondecode(data.http.private_dns_zone.response_body)
}

output "aks_cluster_response" {
  value = jsondecode(data.http.aks_cluster.response_body)
}

output "aks_node_pool_response" {
  value = jsondecode(data.http.aks_node_pool.response_body)
}

output "func_plan_response" {
  value = jsondecode(data.http.func_plan.response_body)
}

output "func_app_response" {
  value = jsondecode(data.http.func_app.response_body)
}

output "func_definition_response" {
  value = jsondecode(data.http.func_definition.response_body)
}

output "role_definition_response" {
  value = jsondecode(data.http.role_definition.response_body)
}

output "role_assignment_response" {
  value = jsondecode(data.http.role_assignment.response_body)
}

output "role_assignment_rg_response" {
  value = jsondecode(data.http.role_assignment_rg.response_body)
}

output "role_assignments_sub_list_response" {
  value = jsondecode(data.http.role_assignments_sub_list.response_body)
}

output "user_assigned_identity_response" {
  value = jsondecode(data.http.user_assigned_identity.response_body)
}

output "eg_topic_response" {
  value = jsondecode(data.http.eg_topic.response_body)
}

output "eg_subscription_response" {
  value = jsondecode(data.http.eg_subscription.response_body)
}

output "eh_namespace_response" {
  value = jsondecode(data.http.eh_namespace.response_body)
}

output "eh_hub_response" {
  value = jsondecode(data.http.eh_hub.response_body)
}

output "eh_consumer_group_response" {
  value = jsondecode(data.http.eh_consumer_group.response_body)
}

output "eh_receive_response" {
  value = jsondecode(data.http.eh_receive.response_body)
}

output "apim_service_response" {
  value = jsondecode(data.http.apim_service.response_body)
}

output "apim_api_response" {
  value = jsondecode(data.http.apim_api.response_body)
}

output "apim_operation_response" {
  value = jsondecode(data.http.apim_operation.response_body)
}

output "apim_product_response" {
  value = jsondecode(data.http.apim_product.response_body)
}

output "apim_subscription_response" {
  value = jsondecode(data.http.apim_subscription.response_body)
}
