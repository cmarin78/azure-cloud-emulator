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

Write-Host "-- DELETE storage account --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg/providers/Microsoft.Storage/storageAccounts/$Account`?api-version=$ApiStorage"

Write-Host "-- DELETE resource group (async, 202) --"
az rest --method delete --url "$Endpoint/subscriptions/$Sub/resourceGroups/$Rg`?api-version=$ApiRg"

Remove-Item -Force $RgBodyFile, $StorageBodyFile -ErrorAction SilentlyContinue

Write-Host "== Listo =="
