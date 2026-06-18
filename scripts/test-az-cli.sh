#!/usr/bin/env bash
# Smoke-test del emulador usando az CLI.
#
# A diferencia de gcloud, az CLI no tiene un knob simple de
# "api_endpoint_overrides": el provider azurerm de Terraform y el propio
# az CLI esperan poder hacer descubrimiento de metadata ARM
# (GET /metadata/endpoints) y autenticarse contra Azure AD real, ninguna
# de las cuales implementa (todavía) este emulador — ver ROADMAP.md.
#
# La forma práctica de probar cada endpoint hoy es `az rest`: usa el
# token cacheado de tu sesión real de `az login` (no lo valida el
# emulador) pero te deja apuntar a CUALQUIER URL, incluyendo localhost.
#
# Uso:
#   az login            # una sola vez, con cualquier cuenta/tenant
#   ./scripts/test-az-cli.sh [host:puerto]
set -euo pipefail

ENDPOINT="${1:-http://localhost:10000}"
SUB="00000000-0000-0000-0000-000000000000"
RG="emulator-test-rg"
ACCOUNT="emulatorteststorage"
API_RG="2021-04-01"
API_STORAGE="2023-01-01"

echo "== Probando contra ${ENDPOINT} (subscription falsa ${SUB}) =="

echo "-- healthz --"
curl -sf "${ENDPOINT}/healthz"; echo

echo "-- GET subscription (auto-vivify) --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}?api-version=2020-01-01"

echo "-- PUT resource group --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}?api-version=${API_RG}" \
  --body "{\"location\": \"eastus\"}"

echo "-- GET resource group --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}?api-version=${API_RG}"

echo "-- LIST resource groups --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups?api-version=${API_RG}"

echo "-- PUT storage account --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts/${ACCOUNT}?api-version=${API_STORAGE}" \
  --body "{\"location\": \"eastus\", \"sku\": {\"name\": \"Standard_LRS\"}, \"kind\": \"StorageV2\"}"

echo "-- GET storage account --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts/${ACCOUNT}?api-version=${API_STORAGE}"

echo "-- LIST storage accounts --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts?api-version=${API_STORAGE}"

echo "-- PUT blob container (data plane) --"
az rest --method put \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container?restype=container"

echo "-- GET blob container --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container?restype=container"

echo "-- LIST blob containers (account) --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.blob/?comp=list"

echo "-- PUT blob --"
az rest --method put \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container/hello.txt" \
  --headers "x-ms-blob-type=BlockBlob" \
  --body "hola mundo desde az rest"

echo "-- LIST blobs in container --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container?restype=container&comp=list"

echo "-- DELETE blob --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container/hello.txt"

echo "-- DELETE blob container --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.blob/smoketest-container?restype=container"

QUEUE="smoketest-queue"

echo "-- PUT queue (data plane) --"
az rest --method put \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}"

echo "-- GET queue metadata --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}?comp=metadata"

echo "-- LIST queues (account) --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.queue/?comp=list"

echo "-- PUT message --"
az rest --method post \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}/messages" \
  --body "hola mundo desde az rest"

echo "-- PEEK message (no lo reserva, no hay popReceipt) --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}/messages?peekonly=true"

echo "-- GET message (dequeue: reserva con popReceipt + visibilitytimeout) --"
GET_RESPONSE=$(az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}/messages?numofmessages=1&visibilitytimeout=30")
echo "${GET_RESPONSE}"
MESSAGE_ID=$(echo "${GET_RESPONSE}" | grep -oP '"messageId"\s*:\s*"\K[^"]+' | head -1)
POP_RECEIPT=$(echo "${GET_RESPONSE}" | grep -oP '"popReceipt"\s*:\s*"\K[^"]+' | head -1)

echo "-- DELETE message (requiere el popReceipt de la última lectura) --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}/messages/${MESSAGE_ID}?popreceipt=${POP_RECEIPT}"

echo "-- DELETE queue --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}"

echo "-- DELETE storage account --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts/${ACCOUNT}?api-version=${API_STORAGE}"

echo "-- DELETE resource group (async, 202) --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}?api-version=${API_RG}"

echo "== Listo =="
