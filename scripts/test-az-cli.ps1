# Smoke-test del emulador usando az CLI (PowerShell).
#
# Igual que la versión .sh: usa `az rest` para apuntar directamente a
# localhost con el token cacheado de tu sesión real de `az login`. No
# requiere `az cloud register` (que necesitaría que el emulador implemente
# descubrimiento de metadata ARM + Azure AD, lo cual no hace todavía).
#
# Los cuerpos JSON se escriben a archivos temporales y se pasan con
# `--body @archivo`: el shim az.cmd en Windows destroza las comillas dobles
# embebidas en argumentos --body inline (confirmado con --debug: una cadena
# como '{"location": "eastus"}' llega a az ya como '{location: eastus}',
# sin comillas), así que pasar el JSON inline no es confiable en este
# sistema. El truco @archivo evita ese problema de parseo de argumentos.
#
# Uso:
#   az login
#   .\scripts\test-az-cli.ps1 [-Endpoint http://localhost:10000]
param(
    [string]$Endpoint = "http://localhost:10000"
)

$ErrorActionPreference = "Stop"

$Sub = "00000000-0000-0000-0000-000000000000"
$Rg = "emulator-test-rg"
$Account = "emulatorteststorage"
$ApiRg = "2021-04-01"
$ApiStorage = "2023-01-01"

$RgBodyFile = New-TemporaryFile
$StorageBodyFile = New-TemporaryFile
'{"location": "eastus"}' | Set-Content -NoNewline -Path $RgBodyFile
'{"location": "eastus", "sku": {"name": "Standard_LRS"}, "kind": "StorageV2"}' | Set-Content -NoNewline -Path $StorageBodyFile

Write-Host "== Probando contra $Endpoint (subscription falsa $Sub) =="

Write-Host "-- healthz --"
Invoke-RestMethod -Uri "$Endpoint/healthz"

Write-Host "-- GET subscription (auto-vivify) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub`?api-version=2020-01-01"

Write-Host "-- PUT resource group --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg`?api-version=$ApiRg" --body "@$RgBodyFile"

Write-Host "-- GET resource group --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg`?api-version=$ApiRg"

Write-Host "-- LIST resource groups --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups`?api-version=$ApiRg"

Write-Host "-- PUT storage account --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$Account`?api-version=$ApiStorage" --body "@$StorageBodyFile"

Write-Host "-- GET storage account --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$Account`?api-version=$ApiStorage"

Write-Host "-- LIST storage accounts --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts`?api-version=$ApiStorage"

Write-Host "-- PUT blob container (data plane) --"
az rest --method put --url "$Endpoint/$Account.blob/smoketest-container`?restype=container"

Write-Host "-- GET blob container --"
az rest --method get --url "$Endpoint/$Account.blob/smoketest-container`?restype=container"

Write-Host "-- LIST blob containers (account) --"
az rest --method get --url "$Endpoint/$Account.blob/`?comp=list"

Write-Host "-- PUT blob --"
az rest --method put --url "$Endpoint/$Account.blob/smoketest-container/hello.txt" --headers "x-ms-blob-type=BlockBlob" --body "hola mundo desde az rest"

Write-Host "-- LIST blobs in container --"
# Nota: '&' literal en --url rompe az.cmd en Windows (cmd.exe lo trata como
# separador de comandos al re-parsear el argumento), igual de espiritu que
# el bug de comillas embebidas documentado arriba. Percent-encodear el '&'
# como %26 NO sirve aqui: Go (al igual que la mayoria de parsers de query
# string) separa los pares clave=valor por '&' LITERAL antes de decodificar
# percent-encoding, asi que "%26" termina como parte del VALOR de
# 'restype' en vez de separar 'restype' de 'comp', y el segundo parametro
# nunca llega. La salida es usar Invoke-RestMethod (cmdlet nativo de
# PowerShell, sin pasar por cmd.exe) para esta llamada puntual; el emulador
# no valida el token de auth, asi que perder la demostracion de "az rest"
# en esta unica linea no afecta la cobertura real del smoke test.
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Account.blob/smoketest-container?restype=container&comp=list" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE blob --"
az rest --method delete --url "$Endpoint/$Account.blob/smoketest-container/hello.txt"

Write-Host "-- DELETE blob container --"
az rest --method delete --url "$Endpoint/$Account.blob/smoketest-container`?restype=container"

$Queue = "smoketest-queue"

Write-Host "-- PUT queue (data plane) --"
az rest --method put --url "$Endpoint/$Account.queue/$Queue"

Write-Host "-- GET queue metadata --"
az rest --method get --url "$Endpoint/$Account.queue/$Queue`?comp=metadata"

Write-Host "-- LIST queues (account) --"
az rest --method get --url "$Endpoint/$Account.queue/`?comp=list"

Write-Host "-- PUT message --"
az rest --method post --url "$Endpoint/$Account.queue/$Queue/messages" --body "hola mundo desde az rest"

Write-Host "-- PEEK message (no lo reserva, no hay popReceipt) --"
az rest --method get --url "$Endpoint/$Account.queue/$Queue/messages`?peekonly=true"

Write-Host "-- GET message (dequeue: reserva con popReceipt + visibilitytimeout) --"
# Mismo problema del '&' con az.cmd/cmd.exe documentado arriba para el LIST
# de blobs: numofmessages y visibilitytimeout son dos query params, así que
# usamos Invoke-RestMethod en vez de az rest para esta llamada puntual.
$got = Invoke-RestMethod -Method Get -Uri "$Endpoint/$Account.queue/$Queue/messages?numofmessages=1&visibilitytimeout=30"
$got | ConvertTo-Json -Depth 10
$messageId = $got.value[0].messageId
$popReceipt = $got.value[0].popReceipt

Write-Host "-- DELETE message (requiere el popReceipt de la última lectura) --"
az rest --method delete --url "$Endpoint/$Account.queue/$Queue/messages/$messageId`?popreceipt=$popReceipt"

Write-Host "-- DELETE queue --"
az rest --method delete --url "$Endpoint/$Account.queue/$Queue"

$Table = "smoketesttable"
$TableBodyFile = New-TemporaryFile
$EntityBodyFile = New-TemporaryFile
"{`"TableName`": `"$Table`"}" | Set-Content -NoNewline -Path $TableBodyFile
'{"PartitionKey": "ar", "RowKey": "1", "Name": "Cesar", "Age": 47}' | Set-Content -NoNewline -Path $EntityBodyFile

# Limpieza idempotente: una corrida previa interrumpida puede dejar la tabla
# (o la entidad) ya creada en BoltDB, y la API de table storage usa POST
# (no PUT) para crear ambas, asi que un re-run completo fallaria con
# TableAlreadyExists/EntityAlreadyExists Conflict sin este delete-then-create
# (visto durante la verificacion en vivo de la Fase 14). Los errores se
# ignoran a proposito si el recurso no existia.
Write-Host "-- DELETE entity preexistente (limpieza idempotente, ignora error si no existe) --"
try { Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Account.table/$Table(PartitionKey='ar',RowKey='1')" -Headers @{"If-Match"="*"} | Out-Null } catch {}

Write-Host "-- DELETE table preexistente (limpieza idempotente, ignora error si no existe) --"
try { Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Account.table/Tables('$Table')" | Out-Null } catch {}

Write-Host "-- POST create table (data plane) --"
az rest --method post --url "$Endpoint/$Account.table/Tables" --body "@$TableBodyFile"

Write-Host "-- GET list tables --"
az rest --method get --url "$Endpoint/$Account.table/Tables"

Write-Host "-- POST insert entity --"
az rest --method post --url "$Endpoint/$Account.table/$Table" --body "@$EntityBodyFile"

Write-Host "-- GET entity puntual --"
# La URL de una entidad puntual incluye parentesis y comillas simples
# ("People(PartitionKey='ar',RowKey='1')"), que no confiamos en que
# sobrevivan intactos al re-parseo de az.cmd/cmd.exe en Windows (mismo
# espiritu que el bug del '&' y de las comillas embebidas documentado
# arriba para blob/queue) asi que, igual que esos casos, usamos
# Invoke-RestMethod para todas las operaciones sobre una entidad puntual.
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Account.table/$Table(PartitionKey='ar',RowKey='1')" | ConvertTo-Json -Depth 10

Write-Host "-- GET query entities (`$filter=PartitionKey eq 'ar') --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Account.table/$Table()?`$filter=PartitionKey eq 'ar'" | ConvertTo-Json -Depth 10

Write-Host "-- MERGE entity (PATCH, solo actualiza Age) --"
Invoke-RestMethod -Method Patch -Uri "$Endpoint/$Account.table/$Table(PartitionKey='ar',RowKey='1')" -ContentType "application/json" -Body '{"Age": 48}'

Write-Host "-- GET entity tras merge (Name debe seguir presente) --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Account.table/$Table(PartitionKey='ar',RowKey='1')" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE entity --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Account.table/$Table(PartitionKey='ar',RowKey='1')" -Headers @{"If-Match"="*"}

Write-Host "-- DELETE table --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Account.table/Tables('$Table')"

Remove-Item -Force $TableBodyFile, $EntityBodyFile -ErrorAction SilentlyContinue

Write-Host "-- DELETE storage account --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$Account`?api-version=$ApiStorage"

$ApiNetwork = "2023-09-01"
$ApiCompute = "2023-09-01"
$ApiComputeImages = "2023-04-02"
$Vnet = "smoketest-vnet"
$Subnet = "default"
$Nic = "smoketest-nic"
$Disk = "smoketest-disk"
$Vm = "smoketest-vm"
$Location = "eastus"

$VnetBodyFile = New-TemporaryFile
$SubnetBodyFile = New-TemporaryFile
$NicBodyFile = New-TemporaryFile
$DiskBodyFile = New-TemporaryFile
$VmBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"properties`": {`"addressSpace`": {`"addressPrefixes`": [`"10.0.0.0/16`"]}}}" | Set-Content -NoNewline -Path $VnetBodyFile
'{"properties": {"addressPrefix": "10.0.1.0/24"}}' | Set-Content -NoNewline -Path $SubnetBodyFile

Write-Host "-- PUT virtual network --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet`?api-version=$ApiNetwork" --body "@$VnetBodyFile"

Write-Host "-- GET virtual network --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet`?api-version=$ApiNetwork"

Write-Host "-- PUT subnet --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet/subnets/$Subnet`?api-version=$ApiNetwork" --body "@$SubnetBodyFile"

Write-Host "-- GET subnet --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet/subnets/$Subnet`?api-version=$ApiNetwork"

$SubnetId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet/subnets/$Subnet"
"{`"location`": `"$Location`", `"properties`": {`"ipConfigurations`": [{`"name`": `"ipconfig1`", `"properties`": {`"subnet`": {`"id`": `"$SubnetId`"}}}]}}" | Set-Content -NoNewline -Path $NicBodyFile

Write-Host "-- PUT network interface (asigna IP privada automáticamente) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$Nic`?api-version=$ApiNetwork" --body "@$NicBodyFile"

Write-Host "-- GET network interface --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$Nic`?api-version=$ApiNetwork"

$NicId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$Nic"
"{`"location`": `"$Location`", `"sku`": {`"name`": `"Standard_LRS`"}, `"properties`": {`"diskSizeGB`": 32, `"creationData`": {`"createOption`": `"Empty`"}}}" | Set-Content -NoNewline -Path $DiskBodyFile

Write-Host "-- PUT managed disk --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/disks/$Disk`?api-version=$ApiCompute" --body "@$DiskBodyFile"

Write-Host "-- GET managed disk --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/disks/$Disk`?api-version=$ApiCompute"

Write-Host "-- LIST imágenes del catálogo estático (Canonical Ubuntu 22.04) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Compute/locations/$Location/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus/22_04-lts-gen2/versions`?api-version=$ApiComputeImages"

"{`"location`": `"$Location`", `"properties`": {`"hardwareProfile`": {`"vmSize`": `"Standard_B1s`"}, `"storageProfile`": {`"imageReference`": {`"publisher`": `"Canonical`", `"offer`": `"0001-com-ubuntu-server-jammy`", `"sku`": `"22_04-lts-gen2`", `"version`": `"latest`"}}, `"osProfile`": {`"computerName`": `"smoketestvm`", `"adminUsername`": `"azureuser`", `"adminPassword`": `"P@ssw0rd1234!`"}, `"networkProfile`": {`"networkInterfaces`": [{`"id`": `"$NicId`"}]}}}" | Set-Content -NoNewline -Path $VmBodyFile

Write-Host "-- PUT virtual machine (async, 202 con cuerpo) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$Vm`?api-version=$ApiCompute" --body "@$VmBodyFile"

Write-Host "-- GET virtual machine (la respuesta no debe incluir adminPassword) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$Vm`?api-version=$ApiCompute"

Write-Host "-- POST powerOff virtual machine (async, 202 sin cuerpo) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$Vm/powerOff`?api-version=$ApiCompute"

Write-Host "-- POST start virtual machine (async, 202 sin cuerpo) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$Vm/start`?api-version=$ApiCompute"

Write-Host "-- DELETE virtual machine (async, 202) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$Vm`?api-version=$ApiCompute"

Write-Host "-- DELETE managed disk --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/disks/$Disk`?api-version=$ApiCompute"

Write-Host "-- DELETE network interface --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$Nic`?api-version=$ApiNetwork"

Write-Host "-- DELETE subnet --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet/subnets/$Subnet`?api-version=$ApiNetwork"

Write-Host "-- DELETE virtual network --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$Vnet`?api-version=$ApiNetwork"

Remove-Item -Force $VnetBodyFile, $SubnetBodyFile, $NicBodyFile, $DiskBodyFile, $VmBodyFile -ErrorAction SilentlyContinue

$ApiKeyVault = "2023-07-01"
$Vault = "smoketestkv"
$TenantId = "00000000-0000-0000-0000-000000000000"

$VaultBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"properties`": {`"sku`": {`"family`": `"A`", `"name`": `"standard`"}, `"tenantId`": `"$TenantId`"}}" | Set-Content -NoNewline -Path $VaultBodyFile

Write-Host "-- PUT key vault (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.KeyVault/vaults/$Vault`?api-version=$ApiKeyVault" --body "@$VaultBodyFile"

Write-Host "-- GET key vault --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.KeyVault/vaults/$Vault`?api-version=$ApiKeyVault"

Write-Host "-- LIST key vaults --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.KeyVault/vaults`?api-version=$ApiKeyVault"

Write-Host "-- PUT secret (data plane) --"
Invoke-RestMethod -Method Put -Uri "$Endpoint/$Vault.vault/secrets/smoketest-secret" -ContentType "application/json" -Body '{"value": "super-secreto"}'

Write-Host "-- GET secret --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/secrets/smoketest-secret" | ConvertTo-Json -Depth 10

Write-Host "-- LIST secrets (sin 'value') --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/secrets" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE secret --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Vault.vault/secrets/smoketest-secret"

Write-Host "-- PUT key (data plane, material simulado) --"
Invoke-RestMethod -Method Put -Uri "$Endpoint/$Vault.vault/keys/smoketest-key" -ContentType "application/json" -Body '{"kty": "RSA"}'

Write-Host "-- GET key --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/keys/smoketest-key" | ConvertTo-Json -Depth 10

Write-Host "-- LIST keys --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/keys" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE key --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Vault.vault/keys/smoketest-key"

Write-Host "-- PUT certificate (data plane, material simulado) --"
Invoke-RestMethod -Method Put -Uri "$Endpoint/$Vault.vault/certificates/smoketest-cert" -ContentType "application/json" -Body '{"policy": {}}'

Write-Host "-- GET certificate --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/certificates/smoketest-cert" | ConvertTo-Json -Depth 10

Write-Host "-- LIST certificates --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Vault.vault/certificates" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE certificate --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Vault.vault/certificates/smoketest-cert"

Write-Host "-- DELETE key vault --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.KeyVault/vaults/$Vault`?api-version=$ApiKeyVault"

Remove-Item -Force $VaultBodyFile -ErrorAction SilentlyContinue

$ApiServiceBus = "2021-11-01"
$Namespace = "smoketestns"
$QueueSb = "smoketest-sbqueue"
$Topic = "smoketest-topic"
$Subscription = "smoketest-sub"

$NsBodyFile = New-TemporaryFile
$SbQueueBodyFile = New-TemporaryFile
$TopicBodyFile = New-TemporaryFile
$SubBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $NsBodyFile
'{"properties": {}}' | Set-Content -NoNewline -Path $SbQueueBodyFile
'{"properties": {}}' | Set-Content -NoNewline -Path $TopicBodyFile
'{"properties": {}}' | Set-Content -NoNewline -Path $SubBodyFile

Write-Host "-- PUT Service Bus namespace (async, 202) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace`?api-version=$ApiServiceBus" --body "@$NsBodyFile"

Write-Host "-- GET Service Bus namespace --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace`?api-version=$ApiServiceBus"

Write-Host "-- PUT Service Bus queue --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/queues/$QueueSb`?api-version=$ApiServiceBus" --body "@$SbQueueBodyFile"

Write-Host "-- SEND mensaje a la cola (data plane) --"
Invoke-RestMethod -Method Post -Uri "$Endpoint/$Namespace.servicebus/$QueueSb/messages" -ContentType "application/json" -Body '{"body": "hola desde Service Bus"}' | ConvertTo-Json -Compress

Write-Host "-- RECEIVE mensaje (peek-lock) --"
$sbMsg = Invoke-RestMethod -Method Get -Uri "$Endpoint/$Namespace.servicebus/$QueueSb/messages?peeklock=true"
$sbMsg | ConvertTo-Json -Depth 10
$sbMessageId = $sbMsg.value[0].messageId
$sbLockToken = $sbMsg.value[0].lockToken

Write-Host "-- COMPLETE mensaje (requiere lockToken de la última recepción) --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$Namespace.servicebus/$QueueSb/messages/$sbMessageId`?lockToken=$sbLockToken"

Write-Host "-- DELETE Service Bus queue --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/queues/$QueueSb`?api-version=$ApiServiceBus"

Write-Host "-- PUT Service Bus topic --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/topics/$Topic`?api-version=$ApiServiceBus" --body "@$TopicBodyFile"

Write-Host "-- PUT Service Bus subscription --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/topics/$Topic/subscriptions/$Subscription`?api-version=$ApiServiceBus" --body "@$SubBodyFile"

Write-Host "-- SEND mensaje al topic (fan-out a sus subscriptions) --"
Invoke-RestMethod -Method Post -Uri "$Endpoint/$Namespace.servicebus/$Topic/messages" -ContentType "application/json" -Body '{"body": "hola desde el topic"}' | ConvertTo-Json -Compress

Write-Host "-- RECEIVE mensaje desde la subscription --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$Namespace.servicebus/$Topic/subscriptions/$Subscription/messages?peeklock=true" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE Service Bus subscription --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/topics/$Topic/subscriptions/$Subscription`?api-version=$ApiServiceBus"

Write-Host "-- DELETE Service Bus topic --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace/topics/$Topic`?api-version=$ApiServiceBus"

Write-Host "-- DELETE Service Bus namespace --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ServiceBus/namespaces/$Namespace`?api-version=$ApiServiceBus"

Remove-Item -Force $NsBodyFile, $SbQueueBodyFile, $TopicBodyFile, $SubBodyFile -ErrorAction SilentlyContinue

$ApiCosmosDb = "2023-04-15"
$CosmosAccount = "smoketestcosmos"
$CosmosDb = "smoketestdb"
$CosmosContainer = "smoketestcontainer"

$CosmosAcctBodyFile = New-TemporaryFile
$CosmosDbBodyFile = New-TemporaryFile
$CosmosContainerBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $CosmosAcctBodyFile
"{`"properties`": {`"resource`": {`"id`": `"$CosmosDb`"}}}" | Set-Content -NoNewline -Path $CosmosDbBodyFile
"{`"properties`": {`"resource`": {`"id`": `"$CosmosContainer`", `"partitionKey`": {`"paths`": [`"/pk`"]}}}}" | Set-Content -NoNewline -Path $CosmosContainerBodyFile

Write-Host "-- PUT cuenta de Cosmos DB (async, 202) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount`?api-version=$ApiCosmosDb" --body "@$CosmosAcctBodyFile"

Write-Host "-- GET cuenta de Cosmos DB --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount`?api-version=$ApiCosmosDb"

Write-Host "-- PUT base de datos SQL --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount/sqlDatabases/$CosmosDb`?api-version=$ApiCosmosDb" --body "@$CosmosDbBodyFile"

Write-Host "-- PUT container (requiere partitionKey) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount/sqlDatabases/$CosmosDb/containers/$CosmosContainer`?api-version=$ApiCosmosDb" --body "@$CosmosContainerBodyFile"

Write-Host "-- PUT documento (data plane) --"
Invoke-RestMethod -Method Put -Uri "$Endpoint/$CosmosAccount.documents/dbs/$CosmosDb/colls/$CosmosContainer/docs/smoketest-doc" -ContentType "application/json" -Body '{"pk": "x", "value": 42}' | ConvertTo-Json -Compress

Write-Host "-- GET documento --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$CosmosAccount.documents/dbs/$CosmosDb/colls/$CosmosContainer/docs/smoketest-doc" | ConvertTo-Json -Compress

Write-Host "-- LIST documentos del container --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$CosmosAccount.documents/dbs/$CosmosDb/colls/$CosmosContainer/docs" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE documento --"
Invoke-RestMethod -Method Delete -Uri "$Endpoint/$CosmosAccount.documents/dbs/$CosmosDb/colls/$CosmosContainer/docs/smoketest-doc"

Write-Host "-- DELETE container --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount/sqlDatabases/$CosmosDb/containers/$CosmosContainer`?api-version=$ApiCosmosDb"

Write-Host "-- DELETE base de datos SQL --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount/sqlDatabases/$CosmosDb`?api-version=$ApiCosmosDb"

Write-Host "-- DELETE cuenta de Cosmos DB --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.DocumentDB/databaseAccounts/$CosmosAccount`?api-version=$ApiCosmosDb"

Remove-Item -Force $CosmosAcctBodyFile, $CosmosDbBodyFile, $CosmosContainerBodyFile -ErrorAction SilentlyContinue

$ApiMonitor = "2022-10-01"
$ApiInsights = "2021-08-01"
$Workspace = "smoketestworkspace"
$ActionGroup = "smoketestactiongroup"
$MetricAlert = "smoketestmetricalert"

$WorkspaceBodyFile = New-TemporaryFile
$ActionGroupBodyFile = New-TemporaryFile
$MetricAlertBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $WorkspaceBodyFile
'{"location": "global", "properties": {"groupShortName": "smoketest", "emailReceivers": [{"name": "admin", "emailAddress": "admin@example.com"}], "webhookReceivers": [{"name": "smoketesthook", "serviceUri": "http://localhost:10999/webhook"}]}}' | Set-Content -NoNewline -Path $ActionGroupBodyFile

$ActionGroupId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup"
"{`"location`": `"global`", `"properties`": {`"severity`": 2, `"scopes`": [`"$NicId`"], `"criteria`": {`"odata.type`": `"Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria`"}, `"actions`": [{`"actionGroupId`": `"$ActionGroupId`"}]}}" | Set-Content -NoNewline -Path $MetricAlertBodyFile

Write-Host "-- PUT Log Analytics workspace (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.OperationalInsights/workspaces/$Workspace`?api-version=$ApiMonitor" --body "@$WorkspaceBodyFile"

Write-Host "-- GET Log Analytics workspace --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.OperationalInsights/workspaces/$Workspace`?api-version=$ApiMonitor"

Write-Host "-- LIST Log Analytics workspaces --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.OperationalInsights/workspaces`?api-version=$ApiMonitor"

Write-Host "-- POST sharedKeys (azurerm_log_analytics_workspace primary/secondary_shared_key) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.OperationalInsights/workspaces/$Workspace/sharedKeys`?api-version=$ApiMonitor"

Write-Host "-- POST Log Analytics query (data plane, stub: siempre vacío) --"
Invoke-RestMethod -Method Post -Uri "$Endpoint/v1/workspaces/smoketest-fake-customer-id/query" -ContentType "application/json" -Body '{"query": "AzureActivity | take 1"}' | ConvertTo-Json -Depth 10

Write-Host "-- PUT action group (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup`?api-version=$ApiInsights" --body "@$ActionGroupBodyFile"

Write-Host "-- GET action group --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup`?api-version=$ApiInsights"

Write-Host "-- LIST action groups --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups`?api-version=$ApiInsights"

Write-Host "-- POST createNotifications (Fase 20: dispara un webhook real a http://localhost:10999/webhook) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup/createNotifications`?api-version=$ApiInsights" --body "{}"

Write-Host "-- GET action group (confirma lastNotificationStatus tras el despacho real) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup`?api-version=$ApiInsights"

Write-Host "-- PUT metric alert (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/metricAlerts/$MetricAlert`?api-version=$ApiInsights" --body "@$MetricAlertBodyFile"

Write-Host "-- GET metric alert --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/metricAlerts/$MetricAlert`?api-version=$ApiInsights"

Write-Host "-- LIST metric alerts --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/metricAlerts`?api-version=$ApiInsights"

Write-Host "-- DELETE metric alert --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/metricAlerts/$MetricAlert`?api-version=$ApiInsights"

Write-Host "-- DELETE action group --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Insights/actionGroups/$ActionGroup`?api-version=$ApiInsights"

Write-Host "-- DELETE Log Analytics workspace --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.OperationalInsights/workspaces/$Workspace`?api-version=$ApiMonitor"

Remove-Item -Force $WorkspaceBodyFile, $ActionGroupBodyFile, $MetricAlertBodyFile -ErrorAction SilentlyContinue

$ApiAppService = "2022-03-01"
$Plan = "smoketestplan"
$Site = "smoketestsite"

$PlanBodyFile = New-TemporaryFile
$SiteBodyFile = New-TemporaryFile
$AppSettingsBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"kind`": `"linux`", `"sku`": {`"name`": `"B1`", `"tier`": `"Basic`"}}" | Set-Content -NoNewline -Path $PlanBodyFile

$PlanId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$Plan"
"{`"location`": `"$Location`", `"kind`": `"app,linux`", `"properties`": {`"serverFarmId`": `"$PlanId`", `"siteConfig`": {`"linuxFxVersion`": `"DOCKER|nginx:latest`"}}}" | Set-Content -NoNewline -Path $SiteBodyFile
'{"properties": {"WEBSITES_PORT": "8080"}}' | Set-Content -NoNewline -Path $AppSettingsBodyFile

Write-Host "-- PUT App Service Plan (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$Plan`?api-version=$ApiAppService" --body "@$PlanBodyFile"

Write-Host "-- GET App Service Plan --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$Plan`?api-version=$ApiAppService"

Write-Host "-- LIST App Service Plans --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms`?api-version=$ApiAppService"

Write-Host "-- PUT Web App (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site`?api-version=$ApiAppService" --body "@$SiteBodyFile"

Write-Host "-- GET Web App --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site`?api-version=$ApiAppService"

Write-Host "-- LIST Web Apps --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites`?api-version=$ApiAppService"

Write-Host "-- PUT app settings (StringDictionary, reemplazo completo) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site/config/appsettings`?api-version=$ApiAppService" --body "@$AppSettingsBodyFile"

Write-Host "-- GET app settings --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site/config/appsettings`?api-version=$ApiAppService"

Write-Host "-- POST stop Web App (sync, 200) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site/stop`?api-version=$ApiAppService"

Write-Host "-- POST start Web App (sync, 200) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site/start`?api-version=$ApiAppService"

Write-Host "-- POST restart Web App (sync, 200) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site/restart`?api-version=$ApiAppService"

Write-Host "-- DELETE Web App --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$Site`?api-version=$ApiAppService"

Write-Host "-- DELETE App Service Plan --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$Plan`?api-version=$ApiAppService"

Remove-Item -Force $PlanBodyFile, $SiteBodyFile, $AppSettingsBodyFile -ErrorAction SilentlyContinue

$Nsg = "smoketestnsg"
$Pip = "smoketestpip"
$Lb = "smoketestlb"
$Rt = "smoketestrt"
$DnsZone = "smoketest.internal"

$NsgBodyFile = New-TemporaryFile
$RuleBodyFile = New-TemporaryFile
$PipBodyFile = New-TemporaryFile
$LbBodyFile = New-TemporaryFile
$RtBodyFile = New-TemporaryFile
$RouteBodyFile = New-TemporaryFile
$DnsRecordBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $NsgBodyFile
'{"properties": {"priority": 100, "direction": "Inbound", "access": "Allow", "protocol": "Tcp", "sourceAddressPrefix": "*", "destinationAddressPrefix": "*", "sourcePortRange": "*", "destinationPortRange": "22"}}' | Set-Content -NoNewline -Path $RuleBodyFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $PipBodyFile

Write-Host "-- PUT network security group (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkSecurityGroups/$Nsg`?api-version=$ApiNetwork" --body "@$NsgBodyFile"

Write-Host "-- GET network security group --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkSecurityGroups/$Nsg`?api-version=$ApiNetwork"

Write-Host "-- PUT security rule (sub-recurso) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkSecurityGroups/$Nsg/securityRules/allow-ssh`?api-version=$ApiNetwork" --body "@$RuleBodyFile"

Write-Host "-- GET security rule --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkSecurityGroups/$Nsg/securityRules/allow-ssh`?api-version=$ApiNetwork"

Write-Host "-- PUT public IP address (ARM, sync, IP determinista) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/publicIPAddresses/$Pip`?api-version=$ApiNetwork" --body "@$PipBodyFile"

Write-Host "-- GET public IP address --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/publicIPAddresses/$Pip`?api-version=$ApiNetwork"

$PipId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/publicIPAddresses/$Pip"
$LbId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/loadBalancers/$Lb"
"{`"location`": `"$Location`", `"properties`": {`"frontendIPConfigurations`": [{`"name`": `"frontend1`", `"properties`": {`"publicIPAddress`": {`"id`": `"$PipId`"}}}], `"backendAddressPools`": [{`"name`": `"backend1`"}], `"loadBalancingRules`": [{`"name`": `"rule1`", `"properties`": {`"frontendIPConfiguration`": {`"id`": `"$LbId/frontendIPConfigurations/frontend1`"}, `"backendAddressPool`": {`"id`": `"$LbId/backendAddressPools/backend1`"}, `"protocol`": `"Tcp`", `"frontendPort`": 80, `"backendPort`": 8080}}]}}" | Set-Content -NoNewline -Path $LbBodyFile

Write-Host "-- PUT load balancer (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/loadBalancers/$Lb`?api-version=$ApiNetwork" --body "@$LbBodyFile"

Write-Host "-- GET load balancer --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/loadBalancers/$Lb`?api-version=$ApiNetwork"

"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $RtBodyFile
'{"properties": {"addressPrefix": "0.0.0.0/0", "nextHopType": "Internet"}}' | Set-Content -NoNewline -Path $RouteBodyFile

Write-Host "-- PUT route table (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/routeTables/$Rt`?api-version=$ApiNetwork" --body "@$RtBodyFile"

Write-Host "-- PUT route (sub-recurso) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/routeTables/$Rt/routes/to-internet`?api-version=$ApiNetwork" --body "@$RouteBodyFile"

Write-Host "-- GET route table (debe traer la route anidada) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/routeTables/$Rt`?api-version=$ApiNetwork"

Write-Host "-- PUT private DNS zone (location forzada a 'global') --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/privateDnsZones/$DnsZone`?api-version=$ApiNetwork" --body "{}"

'{"properties": {"ttl": 300, "aRecords": [{"ipv4Address": "10.0.0.4"}]}}' | Set-Content -NoNewline -Path $DnsRecordBodyFile

Write-Host "-- PUT A record (sub-recurso, recordType en la ruta) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/privateDnsZones/$DnsZone/A/www`?api-version=$ApiNetwork" --body "@$DnsRecordBodyFile"

Write-Host "-- GET private DNS zone (numberOfRecordSets debe ser 1) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/privateDnsZones/$DnsZone`?api-version=$ApiNetwork"

Write-Host "-- DELETE A record --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/privateDnsZones/$DnsZone/A/www`?api-version=$ApiNetwork"

Write-Host "-- DELETE private DNS zone --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/privateDnsZones/$DnsZone`?api-version=$ApiNetwork"

Write-Host "-- DELETE route table --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/routeTables/$Rt`?api-version=$ApiNetwork"

Write-Host "-- DELETE load balancer --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/loadBalancers/$Lb`?api-version=$ApiNetwork"

Write-Host "-- DELETE public IP address --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/publicIPAddresses/$Pip`?api-version=$ApiNetwork"

Write-Host "-- DELETE network security group --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkSecurityGroups/$Nsg`?api-version=$ApiNetwork"

Remove-Item -Force $NsgBodyFile, $RuleBodyFile, $PipBodyFile, $LbBodyFile, $RtBodyFile, $RouteBodyFile, $DnsRecordBodyFile -ErrorAction SilentlyContinue

$ApiAks = "2023-10-01"
$Cluster = "smoketestaks"
$NodePool = "userpool"

$ClusterBodyFile = New-TemporaryFile
$NodePoolBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"identity`": {`"type`": `"SystemAssigned`"}, `"properties`": {`"dnsPrefix`": `"$Cluster`"}}" | Set-Content -NoNewline -Path $ClusterBodyFile
'{"properties": {"vmSize": "Standard_DS2_v2", "count": 2, "mode": "User"}}' | Set-Content -NoNewline -Path $NodePoolBodyFile

Write-Host "-- PUT AKS managed cluster (async, 202 con cuerpo) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster`?api-version=$ApiAks" --body "@$ClusterBodyFile"

Write-Host "-- GET AKS managed cluster (debe traer el pool 'default' sintetizado) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster`?api-version=$ApiAks"

Write-Host "-- LIST AKS managed clusters --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters`?api-version=$ApiAks"

Write-Host "-- PUT AKS agent pool (sub-recurso, async, 202) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster/agentPools/$NodePool`?api-version=$ApiAks" --body "@$NodePoolBodyFile"

Write-Host "-- GET AKS agent pool --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster/agentPools/$NodePool`?api-version=$ApiAks"

Write-Host "-- LIST AKS agent pools (default + userpool) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster/agentPools`?api-version=$ApiAks"

Write-Host "-- POST listClusterUserCredential (kubeconfig base64) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster/listClusterUserCredential`?api-version=$ApiAks"

Write-Host "-- DELETE AKS agent pool --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster/agentPools/$NodePool`?api-version=$ApiAks"

Write-Host "-- DELETE AKS managed cluster (cascada sobre agentPools restantes) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerService/managedClusters/$Cluster`?api-version=$ApiAks"

Remove-Item -Force $ClusterBodyFile, $NodePoolBodyFile -ErrorAction SilentlyContinue

$ApiFunctions = "2022-03-01"
$FuncPlan = "smoketestfuncplan"
$FuncApp = "smoketestfuncapp"
$FuncName = "HttpTrigger1"

$FuncPlanBodyFile = New-TemporaryFile
$FuncAppBodyFile = New-TemporaryFile
$FuncDefBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"sku`": {`"name`": `"Y1`", `"tier`": `"Dynamic`"}}" | Set-Content -NoNewline -Path $FuncPlanBodyFile

$FuncPlanId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$FuncPlan"
"{`"location`": `"$Location`", `"kind`": `"functionapp,linux`", `"properties`": {`"serverFarmId`": `"$FuncPlanId`"}}" | Set-Content -NoNewline -Path $FuncAppBodyFile
'{"properties": {"language": "python", "config": {"bindings": [{"type": "httpTrigger", "direction": "in", "authLevel": "function"}]}}}' | Set-Content -NoNewline -Path $FuncDefBodyFile

Write-Host "-- PUT App Service Plan para Functions (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$FuncPlan`?api-version=$ApiAppService" --body "@$FuncPlanBodyFile"

Write-Host "-- PUT Function App (Microsoft.Web/sites con kind=functionapp; cubierto por appservice) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp`?api-version=$ApiFunctions" --body "@$FuncAppBodyFile"

Write-Host "-- PUT function definition (sub-recurso Microsoft.Web/sites/functions) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/functions/$FuncName`?api-version=$ApiFunctions" --body "@$FuncDefBodyFile"

Write-Host "-- GET function definition --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/functions/$FuncName`?api-version=$ApiFunctions"

Write-Host "-- LIST function definitions --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/functions`?api-version=$ApiFunctions"

Write-Host "-- POST syncfunctiontriggers (sync, 204 sin cuerpo) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/syncfunctiontriggers`?api-version=$ApiFunctions"

Write-Host "-- POST host/default/listkeys (masterKey + functionKeys.default deterministas) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/host/default/listkeys`?api-version=$ApiFunctions"

Write-Host "-- DELETE function definition --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp/functions/$FuncName`?api-version=$ApiFunctions"

Write-Host "-- DELETE Function App --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/sites/$FuncApp`?api-version=$ApiFunctions"

Write-Host "-- DELETE App Service Plan para Functions --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Web/serverfarms/$FuncPlan`?api-version=$ApiAppService"

Remove-Item -Force $FuncPlanBodyFile, $FuncAppBodyFile, $FuncDefBodyFile -ErrorAction SilentlyContinue

$ApiAuthz = "2022-04-01"
$AppDisplayName = "smoketest-app"
$RoleDefId = "55555555-5555-5555-5555-555555555555"
$RoleAssignName = "66666666-6666-6666-6666-666666666666"
$RoleAssignRgName = "77777777-7777-7777-7777-777777777777"

$AppBodyFile = New-TemporaryFile
$SpBodyFile = New-TemporaryFile
$RoleDefBodyFile = New-TemporaryFile
$RoleAssignBodyFile = New-TemporaryFile
"{`"displayName`": `"$AppDisplayName`"}" | Set-Content -NoNewline -Path $AppBodyFile

Write-Host "-- POST application (Microsoft Graph, sin directorio real detrás) --"
$app = az rest --method post --url "$Endpoint/v1.0/applications" --body "@$AppBodyFile" | ConvertFrom-Json
$app | ConvertTo-Json
$AppId = $app.appId
$AppObjectId = $app.id

Write-Host "-- GET application --"
az rest --method get --url "$Endpoint/v1.0/applications/$AppObjectId"

"{`"appId`": `"$AppId`", `"displayName`": `"$AppDisplayName`"}" | Set-Content -NoNewline -Path $SpBodyFile

Write-Host "-- POST service principal explícito (az ad sp create-for-rbac) --"
$sp = az rest --method post --url "$Endpoint/v1.0/servicePrincipals" --body "@$SpBodyFile" | ConvertFrom-Json
$sp | ConvertTo-Json
$SpObjectId = $sp.id

Write-Host "-- GET service principal por filtro de appId (descubrimiento automático que ya usaba azurerm) --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/v1.0/servicePrincipals?`$filter=appId eq '$AppId'" | ConvertTo-Json -Depth 10

$RoleDefFullId = "/subscriptions/$Sub/providers/Microsoft.Authorization/roleDefinitions/$RoleDefId"
"{`"properties`": {`"roleName`": `"smoketest-custom-role`", `"description`": `"rol de prueba`", `"assignableScopes`": [`"/subscriptions/$Sub`"], `"permissions`": [{`"actions`": [`"Microsoft.Storage/storageAccounts/read`"]}]}}" | Set-Content -NoNewline -Path $RoleDefBodyFile

Write-Host "-- PUT role definition (ARM, sync, scope de suscripción) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleDefinitions/$RoleDefId`?api-version=$ApiAuthz" --body "@$RoleDefBodyFile"

Write-Host "-- GET role definition --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleDefinitions/$RoleDefId`?api-version=$ApiAuthz"

Write-Host "-- LIST role definitions --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleDefinitions`?api-version=$ApiAuthz"

"{`"properties`": {`"roleDefinitionId`": `"$RoleDefFullId`", `"principalId`": `"$SpObjectId`"}}" | Set-Content -NoNewline -Path $RoleAssignBodyFile

Write-Host "-- PUT role assignment (scope de suscripción) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleAssignments/$RoleAssignName`?api-version=$ApiAuthz" --body "@$RoleAssignBodyFile"

Write-Host "-- GET role assignment --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleAssignments/$RoleAssignName`?api-version=$ApiAuthz"

Write-Host "-- PUT role assignment (scope de resource group, mismo principal) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Authorization/roleAssignments/$RoleAssignRgName`?api-version=$ApiAuthz" --body "@$RoleAssignBodyFile"

Write-Host "-- LIST role assignments a nivel de suscripción (NO debe incluir la de resource group) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleAssignments`?api-version=$ApiAuthz"

Write-Host "-- LIST role assignments a nivel de resource group --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Authorization/roleAssignments`?api-version=$ApiAuthz"

Write-Host "-- DELETE role assignment (resource group) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Authorization/roleAssignments/$RoleAssignRgName`?api-version=$ApiAuthz"

Write-Host "-- DELETE role assignment (suscripción) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleAssignments/$RoleAssignName`?api-version=$ApiAuthz"

Write-Host "-- DELETE role definition --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/providers/Microsoft.Authorization/roleDefinitions/$RoleDefId`?api-version=$ApiAuthz"

Write-Host "-- DELETE service principal --"
az rest --method delete --url "$Endpoint/v1.0/servicePrincipals/$SpObjectId"

Write-Host "-- DELETE application --"
az rest --method delete --url "$Endpoint/v1.0/applications/$AppObjectId"

Remove-Item -Force $AppBodyFile, $SpBodyFile, $RoleDefBodyFile, $RoleAssignBodyFile -ErrorAction SilentlyContinue

$ApiManagedIdentity = "2023-01-31"
$Identity = "smoketestidentity"

$IdentityBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $IdentityBodyFile

Write-Host "-- PUT user-assigned managed identity (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/$Identity`?api-version=$ApiManagedIdentity" --body "@$IdentityBodyFile"

Write-Host "-- GET user-assigned managed identity (tenantId/principalId/clientId deterministas) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/$Identity`?api-version=$ApiManagedIdentity"

Write-Host "-- LIST user-assigned managed identities --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities`?api-version=$ApiManagedIdentity"

$IdentityVnet = "smoketest-identity-vnet"
$IdentitySubnet = "default"
$IdentityNic = "smoketest-identity-nic"
$IdentityVm = "smoketest-identity-vm"

$IdentityVnetBodyFile = New-TemporaryFile
$IdentitySubnetBodyFile = New-TemporaryFile
$IdentityNicBodyFile = New-TemporaryFile
$IdentityVmBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"properties`": {`"addressSpace`": {`"addressPrefixes`": [`"10.1.0.0/16`"]}}}" | Set-Content -NoNewline -Path $IdentityVnetBodyFile
'{"properties": {"addressPrefix": "10.1.1.0/24"}}' | Set-Content -NoNewline -Path $IdentitySubnetBodyFile

Write-Host "-- PUT virtual network (de soporte, para una VM con identidad) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$IdentityVnet`?api-version=$ApiNetwork" --body "@$IdentityVnetBodyFile"

Write-Host "-- PUT subnet (de soporte) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$IdentityVnet/subnets/$IdentitySubnet`?api-version=$ApiNetwork" --body "@$IdentitySubnetBodyFile"

$IdentitySubnetId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$IdentityVnet/subnets/$IdentitySubnet"
"{`"location`": `"$Location`", `"properties`": {`"ipConfigurations`": [{`"name`": `"ipconfig1`", `"properties`": {`"subnet`": {`"id`": `"$IdentitySubnetId`"}}}]}}" | Set-Content -NoNewline -Path $IdentityNicBodyFile

Write-Host "-- PUT network interface (de soporte) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$IdentityNic`?api-version=$ApiNetwork" --body "@$IdentityNicBodyFile"

$IdentityNicId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$IdentityNic"
"{`"location`": `"$Location`", `"identity`": {`"type`": `"SystemAssigned`"}, `"properties`": {`"hardwareProfile`": {`"vmSize`": `"Standard_B1s`"}, `"storageProfile`": {`"imageReference`": {`"publisher`": `"Canonical`", `"offer`": `"0001-com-ubuntu-server-jammy`", `"sku`": `"22_04-lts-gen2`", `"version`": `"latest`"}}, `"osProfile`": {`"computerName`": `"smoketestidvm`", `"adminUsername`": `"azureuser`", `"adminPassword`": `"P@ssw0rd1234!`"}, `"networkProfile`": {`"networkInterfaces`": [{`"id`": `"$IdentityNicId`"}]}}}" | Set-Content -NoNewline -Path $IdentityVmBodyFile

Write-Host "-- PUT virtual machine con identity.type=SystemAssigned (async, 202; principalId/tenantId deterministas) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$IdentityVm`?api-version=$ApiCompute" --body "@$IdentityVmBodyFile"

Write-Host "-- GET virtual machine (confirma identity.principalId/tenantId no vacios) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$IdentityVm`?api-version=$ApiCompute"

Write-Host "-- DELETE virtual machine (de soporte) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Compute/virtualMachines/$IdentityVm`?api-version=$ApiCompute"

Write-Host "-- DELETE network interface (de soporte) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/networkInterfaces/$IdentityNic`?api-version=$ApiNetwork"

Write-Host "-- DELETE subnet (de soporte) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$IdentityVnet/subnets/$IdentitySubnet`?api-version=$ApiNetwork"

Write-Host "-- DELETE virtual network (de soporte) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Network/virtualNetworks/$IdentityVnet`?api-version=$ApiNetwork"

Write-Host "-- DELETE user-assigned managed identity --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/$Identity`?api-version=$ApiManagedIdentity"

Remove-Item -Force $IdentityBodyFile, $IdentityVnetBodyFile, $IdentitySubnetBodyFile, $IdentityNicBodyFile, $IdentityVmBodyFile -ErrorAction SilentlyContinue

$ApiEventGrid = "2022-06-15"
$ApiEventHub = "2021-11-01"
$EgTopic = "smoketest-egtopic"
$EgSub = "smoketest-egsub"
$EhNamespace = "smoketesteh"
$EhHub = "smoketest-hub"
$EhCg = "smoketest-cg"

$EgTopicBodyFile = New-TemporaryFile
$EgSubBodyFile = New-TemporaryFile
$EhNsBodyFile = New-TemporaryFile
"{`"location`": `"$Location`"}" | Set-Content -NoNewline -Path $EgTopicBodyFile
'{"properties": {"destination": {"endpointType": "WebHook", "properties": {"endpointUrl": "http://localhost:10999/webhook"}}}}' | Set-Content -NoNewline -Path $EgSubBodyFile
"{`"location`": `"$Location`", `"sku`": {`"name`": `"Standard`"}}" | Set-Content -NoNewline -Path $EhNsBodyFile

Write-Host "-- PUT Event Grid topic (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic`?api-version=$ApiEventGrid" --body "@$EgTopicBodyFile"

Write-Host "-- GET Event Grid topic --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic`?api-version=$ApiEventGrid"

Write-Host "-- LIST Event Grid topics --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics`?api-version=$ApiEventGrid"

Write-Host "-- PUT event subscription (extension resource, webhook real a http://localhost:10999/webhook) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic/providers/Microsoft.EventGrid/eventSubscriptions/$EgSub`?api-version=$ApiEventGrid" --body "@$EgSubBodyFile"

Write-Host "-- POST publish evento al topic (data plane, dispara el webhook de forma asíncrona) --"
Invoke-RestMethod -Method Post -Uri "$Endpoint/$EgTopic.eventgrid/api/events" -ContentType "application/json" -Body '[{"id": "1", "eventType": "Smoketest.Event", "subject": "/smoketest", "data": {"x": 1}, "eventTime": "2026-06-22T00:00:00Z", "dataVersion": "1.0"}]' | ConvertTo-Json -Compress

Write-Host "-- Espera breve para que el despacho asíncrono del webhook termine --"
Start-Sleep -Seconds 1

Write-Host "-- GET event subscription (confirma lastDeliveryStatus tras el despacho real) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic/providers/Microsoft.EventGrid/eventSubscriptions/$EgSub`?api-version=$ApiEventGrid"

Write-Host "-- DELETE event subscription --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic/providers/Microsoft.EventGrid/eventSubscriptions/$EgSub`?api-version=$ApiEventGrid"

Write-Host "-- DELETE Event Grid topic --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventGrid/topics/$EgTopic`?api-version=$ApiEventGrid"

Remove-Item -Force $EgTopicBodyFile, $EgSubBodyFile -ErrorAction SilentlyContinue

Write-Host "-- PUT Event Hubs namespace (async, 202) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace`?api-version=$ApiEventHub" --body "@$EhNsBodyFile"

Write-Host "-- GET Event Hubs namespace --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace`?api-version=$ApiEventHub"

Write-Host "-- PUT event hub (sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace/eventhubs/$EhHub`?api-version=$ApiEventHub" --body "{}"

Write-Host "-- PUT consumer group (sub-sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace/eventhubs/$EhHub/consumergroups/$EhCg`?api-version=$ApiEventHub" --body "{}"

Write-Host "-- POST enviar evento (data plane) --"
Invoke-RestMethod -Method Post -Uri "$Endpoint/$EhNamespace.eventhub/$EhHub/messages" -Body "hola desde Event Hubs"

Write-Host "-- GET recibir eventos (offset por defecto) --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$EhNamespace.eventhub/$EhHub/messages" | ConvertTo-Json -Depth 10

Write-Host "-- GET recibir eventos vía consumer group --"
Invoke-RestMethod -Method Get -Uri "$Endpoint/$EhNamespace.eventhub/$EhHub/consumergroups/$EhCg/messages" | ConvertTo-Json -Depth 10

Write-Host "-- DELETE consumer group --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace/eventhubs/$EhHub/consumergroups/$EhCg`?api-version=$ApiEventHub"

Write-Host "-- DELETE event hub --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace/eventhubs/$EhHub`?api-version=$ApiEventHub"

Write-Host "-- DELETE Event Hubs namespace --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.EventHub/namespaces/$EhNamespace`?api-version=$ApiEventHub"

Remove-Item -Force $EhNsBodyFile -ErrorAction SilentlyContinue

$ApiApim = "2022-08-01"
$ApimSvc = "smoketestapim"
$ApimApi = "echo"
$ApimOp = "get-echo"
$ApimProduct = "starter"
$ApimSub = "starter-sub"
$ApimProductId = "/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/products/$ApimProduct"

$ApimSvcBodyFile = New-TemporaryFile
$ApimApiBodyFile = New-TemporaryFile
$ApimOpBodyFile = New-TemporaryFile
$ApimProductBodyFile = New-TemporaryFile
$ApimSubBodyFile = New-TemporaryFile
"{`"location`": `"$Location`", `"sku`": {`"name`": `"Developer`", `"capacity`": 1}, `"properties`": {`"publisherEmail`": `"admin@example.com`", `"publisherName`": `"Contoso`"}}" | Set-Content -NoNewline -Path $ApimSvcBodyFile
'{"properties": {"displayName": "Echo API", "path": "echo", "serviceUrl": "https://backend.example.com"}}' | Set-Content -NoNewline -Path $ApimApiBodyFile
'{"properties": {"displayName": "GET echo", "method": "GET", "urlTemplate": "/{id}"}}' | Set-Content -NoNewline -Path $ApimOpBodyFile
'{"properties": {"displayName": "Starter"}}' | Set-Content -NoNewline -Path $ApimProductBodyFile
"{`"properties`": {`"displayName`": `"Starter subscription`", `"scope`": `"$ApimProductId`"}}" | Set-Content -NoNewline -Path $ApimSubBodyFile

Write-Host "-- PUT API Management service instance (async, 202 con cuerpo) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc`?api-version=$ApiApim" --body "@$ApimSvcBodyFile"

Write-Host "-- GET API Management service instance (gatewayUrl/portalUrl deterministas) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc`?api-version=$ApiApim"

Write-Host "-- LIST API Management service instances --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service`?api-version=$ApiApim"

Write-Host "-- PUT API (sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi`?api-version=$ApiApim" --body "@$ApimApiBodyFile"

Write-Host "-- GET API --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi`?api-version=$ApiApim"

Write-Host "-- PUT API operation (sub-sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi/operations/$ApimOp`?api-version=$ApiApim" --body "@$ApimOpBodyFile"

Write-Host "-- GET API operation --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi/operations/$ApimOp`?api-version=$ApiApim"

Write-Host "-- PUT product (sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/products/$ApimProduct`?api-version=$ApiApim" --body "@$ApimProductBodyFile"

Write-Host "-- PUT product-api association --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/products/$ApimProduct/apis/$ApimApi`?api-version=$ApiApim"

Write-Host "-- GET product (debe seguir existiendo tras la asociación) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/products/$ApimProduct`?api-version=$ApiApim"

Write-Host "-- PUT subscription (primaryKey/secondaryKey deterministas) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/subscriptions/$ApimSub`?api-version=$ApiApim" --body "@$ApimSubBodyFile"

Write-Host "-- GET subscription --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/subscriptions/$ApimSub`?api-version=$ApiApim"

Write-Host "-- DELETE subscription --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/subscriptions/$ApimSub`?api-version=$ApiApim"

Write-Host "-- DELETE product --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/products/$ApimProduct`?api-version=$ApiApim"

Write-Host "-- DELETE API operation --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi/operations/$ApimOp`?api-version=$ApiApim"

Write-Host "-- DELETE API --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc/apis/$ApimApi`?api-version=$ApiApim"

Write-Host "-- DELETE API Management service instance (async, 202; cascada sobre cualquier sub-recurso restante) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ApiManagement/service/$ApimSvc`?api-version=$ApiApim"

Remove-Item -Force $ApimSvcBodyFile, $ApimApiBodyFile, $ApimOpBodyFile, $ApimProductBodyFile, $ApimSubBodyFile -ErrorAction SilentlyContinue

$ApiDeployments = "2021-04-01"
$Deployment = "smoketest-deployment"
$DeployStorage = "smoketestdeploystg"

$DeploymentBodyFile = New-TemporaryFile
$ValidateBodyFile = New-TemporaryFile
"{`"properties`": {`"mode`": `"Incremental`", `"template`": {`"parameters`": {`"storageName`": {`"type`": `"string`", `"defaultValue`": `"$DeployStorage`"}}, `"variables`": {`"skuName`": `"Standard_LRS`"}, `"resources`": [{`"type`": `"Microsoft.Storage/storageAccounts`", `"apiVersion`": `"$ApiStorage`", `"name`": `"[parameters('storageName')]`", `"location`": `"eastus`", `"sku`": {`"name`": `"[variables('skuName')]`"}}], `"outputs`": {`"storageId`": {`"type`": `"string`", `"value`": `"[resourceId('Microsoft.Storage/storageAccounts', parameters('storageName'))]`"}}}, `"parameters`": {}}}" | Set-Content -NoNewline -Path $DeploymentBodyFile
"{`"properties`": {`"mode`": `"Incremental`", `"template`": {`"resources`": [{`"type`": `"Microsoft.Storage/storageAccounts`", `"apiVersion`": `"$ApiStorage`", `"name`": `"$DeployStorage`", `"location`": `"eastus`", `"sku`": {`"name`": `"Standard_LRS`"}}]}}}" | Set-Content -NoNewline -Path $ValidateBodyFile

Write-Host "-- PUT deployment ARM (dispatcher: crea una storage account real vía parameters()/variables()/resourceId()) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Resources/deployments/$Deployment`?api-version=$ApiDeployments" --body "@$DeploymentBodyFile"

Write-Host "-- GET deployment (provisioningState debe ser Succeeded) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Resources/deployments/$Deployment`?api-version=$ApiDeployments"

Write-Host "-- LIST deployment operations (una entrada Succeeded por el recurso despachado) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Resources/deployments/$Deployment/operations`?api-version=$ApiDeployments"

Write-Host "-- GET storage account creada por el deployment (debe existir de verdad) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$DeployStorage`?api-version=$ApiStorage"

Write-Host "-- POST validate deployment (shape-only: no debe crear nada nuevo) --"
az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Resources/deployments/$Deployment/validate`?api-version=$ApiDeployments" --body "@$ValidateBodyFile"

Write-Host "-- DELETE deployment (solo borra el registro del deployment, no la storage account) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Resources/deployments/$Deployment`?api-version=$ApiDeployments"

Write-Host "-- GET storage account tras borrar el deployment (debe seguir existiendo) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$DeployStorage`?api-version=$ApiStorage"

Write-Host "-- DELETE storage account creada por el deployment (limpieza) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$DeployStorage`?api-version=$ApiStorage"

Remove-Item -Force $DeploymentBodyFile, $ValidateBodyFile -ErrorAction SilentlyContinue

# Fase 21 (Logic Apps): a diferencia del placeholder localhost:10999/webhook
# de la Fase 20 (que nunca arranca un listener real), aqui SI arrancamos uno
# (scripts/webhook-counter-listener.ps1) para confirmar de forma positiva
# que el emulador hizo una llamada HTTP real, tanto en el disparo manual
# como en al menos un disparo automatico por recurrencia (interval corto).
$ApiLogic = "2019-05-01"
$Workflow = "smoketest-workflow"
$ListenerPort = 10999

Write-Host "-- Arrancando listener de prueba en localhost:$ListenerPort (cuenta POSTs reales) --"
$ListenerProc = Start-Process -FilePath "powershell" -ArgumentList "-NoProfile", "-File", "$PSScriptRoot\webhook-counter-listener.ps1", "-Port", "$ListenerPort" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 1

try {
    $WorkflowBodyFile = New-TemporaryFile
    "{`"location`": `"eastus`", `"properties`": {`"definition`": {`"`$schema`": `"https://schema.management.azure.com/providers/Microsoft.Logic/schemas/2016-06-01/workflowdefinition.json#`", `"contentVersion`": `"1.0.0.0`", `"triggers`": {`"recurrence`": {`"type`": `"Recurrence`", `"recurrence`": {`"frequency`": `"Second`", `"interval`": 5}}}, `"actions`": {`"callListener`": {`"type`": `"Http`", `"inputs`": {`"method`": `"POST`", `"uri`": `"http://localhost:$ListenerPort/webhook`"}}}}}}" | Set-Content -NoNewline -Path $WorkflowBodyFile

    Write-Host "-- PUT workflow (Recurrence cada 5s + accion Http hacia el listener) --"
    az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Logic/workflows/$Workflow`?api-version=$ApiLogic" --body "@$WorkflowBodyFile"

    Write-Host "-- GET workflow (provisioningState/state deben ser Succeeded/Enabled) --"
    az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Logic/workflows/$Workflow`?api-version=$ApiLogic"

    Write-Host "-- POST trigger run manual (sincrono: dispara una llamada Http real de inmediato) --"
    az rest --method post --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Logic/workflows/$Workflow/triggers/recurrence/run`?api-version=$ApiLogic" --body "{}"

    Write-Host "-- GET workflow tras el run manual (lastRunStatus debe ser 'ok') --"
    az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Logic/workflows/$Workflow`?api-version=$ApiLogic"

    Write-Host "-- Confirmando que el listener de prueba recibio el POST real del run manual --"
    $countAfterManual = [int](Invoke-RestMethod -Method Get -Uri "http://localhost:$ListenerPort/count")
    if ($countAfterManual -lt 1) {
        throw "el listener no recibio ningun POST tras el run manual (count=$countAfterManual)"
    }
    Write-Host "   listener recibio $countAfterManual POST(s) hasta ahora"

    Write-Host "-- Esperando un ciclo de recurrencia automatica (interval=5s) --"
    Start-Sleep -Seconds 7

    $countAfterAuto = [int](Invoke-RestMethod -Method Get -Uri "http://localhost:$ListenerPort/count")
    if ($countAfterAuto -le $countAfterManual) {
        throw "no se detecto ningun disparo automatico por recurrencia (count antes=$countAfterManual, despues=$countAfterAuto)"
    }
    Write-Host "   listener recibio $countAfterAuto POST(s) en total (incluye al menos un disparo automatico)"

    Write-Host "-- DELETE workflow --"
    az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Logic/workflows/$Workflow`?api-version=$ApiLogic"

    Remove-Item -Force $WorkflowBodyFile -ErrorAction SilentlyContinue
} finally {
    Write-Host "-- Deteniendo listener de prueba --"
    Stop-Process -Id $ListenerProc.Id -Force -ErrorAction SilentlyContinue
}

$ApiSql = "2023-08-01-preview"
$SqlServer = "smoketestsqlsrv"
$SqlDb = "smoketestdb"
$SqlFwRule = "AllowAll"

$SqlServerBodyFile = New-TemporaryFile
$SqlDbBodyFile = New-TemporaryFile
$SqlFwBodyFile = New-TemporaryFile
'{"location": "eastus", "properties": {"administratorLogin": "sqladmin", "administratorLoginPassword": "P@ssw0rd1234!"}}' | Set-Content -NoNewline -Path $SqlServerBodyFile
'{"location": "eastus", "sku": {"name": "Basic"}}' | Set-Content -NoNewline -Path $SqlDbBodyFile
'{"properties": {"startIpAddress": "0.0.0.0", "endIpAddress": "255.255.255.255"}}' | Set-Content -NoNewline -Path $SqlFwBodyFile

Write-Host "-- PUT SQL server (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer`?api-version=$ApiSql" --body "@$SqlServerBodyFile"

Write-Host "-- GET SQL server (fullyQualifiedDomainName determinista) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer`?api-version=$ApiSql"

Write-Host "-- LIST SQL servers --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers`?api-version=$ApiSql"

Write-Host "-- PUT SQL database (sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/databases/$SqlDb`?api-version=$ApiSql" --body "@$SqlDbBodyFile"

Write-Host "-- GET SQL database --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/databases/$SqlDb`?api-version=$ApiSql"

Write-Host "-- LIST SQL databases --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/databases`?api-version=$ApiSql"

Write-Host "-- PUT SQL firewall rule (sub-recurso, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/firewallRules/$SqlFwRule`?api-version=$ApiSql" --body "@$SqlFwBodyFile"

Write-Host "-- GET SQL firewall rule --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/firewallRules/$SqlFwRule`?api-version=$ApiSql"

Write-Host "-- LIST SQL firewall rules --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/firewallRules`?api-version=$ApiSql"

Write-Host "-- DELETE SQL firewall rule --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/firewallRules/$SqlFwRule`?api-version=$ApiSql"

Write-Host "-- DELETE SQL database --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer/databases/$SqlDb`?api-version=$ApiSql"

Write-Host "-- DELETE SQL server --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Sql/servers/$SqlServer`?api-version=$ApiSql"

Remove-Item -Force $SqlServerBodyFile, $SqlDbBodyFile, $SqlFwBodyFile -ErrorAction SilentlyContinue

$ApiAcr = "2023-07-01"
$AcrRegistry = "smoketestacr"

$AcrBodyFile = New-TemporaryFile
'{"location": "eastus", "sku": {"name": "Basic"}, "properties": {"adminUserEnabled": true}}' | Set-Content -NoNewline -Path $AcrBodyFile

Write-Host "-- PUT Container Registry (ARM, sync) --"
az rest --method put --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerRegistry/registries/$AcrRegistry`?api-version=$ApiAcr" --body "@$AcrBodyFile"

Write-Host "-- GET Container Registry (loginServer determinista) --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerRegistry/registries/$AcrRegistry`?api-version=$ApiAcr"

Write-Host "-- LIST Container Registries --"
az rest --method get --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerRegistry/registries`?api-version=$ApiAcr"

Write-Host "-- DELETE Container Registry --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.ContainerRegistry/registries/$AcrRegistry`?api-version=$ApiAcr"

Remove-Item -Force $AcrBodyFile -ErrorAction SilentlyContinue

Write-Host "-- DELETE resource group (async, 202) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg`?api-version=$ApiRg"

Remove-Item -Force $RgBodyFile, $StorageBodyFile -ErrorAction SilentlyContinue

Write-Host "== Listo =="
