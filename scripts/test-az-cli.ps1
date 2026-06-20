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
'{"location": "global", "properties": {"groupShortName": "smoketest", "emailReceivers": [{"name": "admin", "emailAddress": "admin@example.com"}]}}' | Set-Content -NoNewline -Path $ActionGroupBodyFile

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

Write-Host "-- DELETE resource group (async, 202) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg`?api-version=$ApiRg"

Remove-Item -Force $RgBodyFile, $StorageBodyFile -ErrorAction SilentlyContinue

Write-Host "== Listo =="
