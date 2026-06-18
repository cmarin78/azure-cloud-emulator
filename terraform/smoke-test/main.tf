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
