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

# IDs de Microsoft.Network/Microsoft.Compute construidos a mano siguiendo el
# shape estándar de ARM: como el emulador no tiene un provider azurerm real
# (ver comentario al inicio del archivo), no hay un recurso de Terraform que
# nos dé esta cadena automáticamente como atributo — así que se construye
# igual que en scripts/test-az-cli.sh/.ps1 (SUBNET_ID/NIC_ID).
locals {
  subnet_id = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/virtualNetworks/${var.vnet}/subnets/${var.subnet}"
  nic_id    = "/subscriptions/${var.subscription_id}/resourceGroups/${var.resource_group}/providers/Microsoft.Network/networkInterfaces/${var.nic}"
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
