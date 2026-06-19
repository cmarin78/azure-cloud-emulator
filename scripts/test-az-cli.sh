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
MESSAGE_ID=$(echo "${GET_RESPONSE}" | sed -n 's/.*"messageId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
POP_RECEIPT=$(echo "${GET_RESPONSE}" | sed -n 's/.*"popReceipt"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)

echo "-- DELETE message (requiere el popReceipt de la última lectura) --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}/messages/${MESSAGE_ID}?popreceipt=${POP_RECEIPT}"

echo "-- DELETE queue --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.queue/${QUEUE}"

TABLE="smoketesttable"

echo "-- POST create table (data plane) --"
az rest --method post \
  --url "${ENDPOINT}/${ACCOUNT}.table/Tables" \
  --body "{\"TableName\": \"${TABLE}\"}"

echo "-- GET list tables --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.table/Tables"

echo "-- POST insert entity --"
az rest --method post \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}" \
  --body "{\"PartitionKey\": \"ar\", \"RowKey\": \"1\", \"Name\": \"Cesar\", \"Age\": 47}"

echo "-- GET entity puntual --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}(PartitionKey='ar',RowKey='1')"

echo "-- GET query entities (\$filter=PartitionKey eq 'ar') --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}()?\$filter=PartitionKey eq 'ar'"

echo "-- MERGE entity (PATCH, solo actualiza Age) --"
az rest --method patch \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}(PartitionKey='ar',RowKey='1')" \
  --body "{\"Age\": 48}"

echo "-- GET entity tras merge (Name debe seguir presente) --"
az rest --method get \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}(PartitionKey='ar',RowKey='1')"

echo "-- DELETE entity --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.table/${TABLE}(PartitionKey='ar',RowKey='1')" \
  --headers "If-Match=*"

echo "-- DELETE table --"
az rest --method delete \
  --url "${ENDPOINT}/${ACCOUNT}.table/Tables('${TABLE}')"

echo "-- DELETE storage account --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts/${ACCOUNT}?api-version=${API_STORAGE}"

API_NETWORK="2023-09-01"
API_COMPUTE="2023-09-01"
API_COMPUTE_IMAGES="2023-04-02"
VNET="smoketest-vnet"
SUBNET="default"
NIC="smoketest-nic"
DISK="smoketest-disk"
VM="smoketest-vm"
LOCATION="eastus"

echo "-- PUT virtual network --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}?api-version=${API_NETWORK}" \
  --body "{\"location\": \"${LOCATION}\", \"properties\": {\"addressSpace\": {\"addressPrefixes\": [\"10.0.0.0/16\"]}}}"

echo "-- GET virtual network --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}?api-version=${API_NETWORK}"

echo "-- PUT subnet --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}/subnets/${SUBNET}?api-version=${API_NETWORK}" \
  --body "{\"properties\": {\"addressPrefix\": \"10.0.1.0/24\"}}"

echo "-- GET subnet --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}/subnets/${SUBNET}?api-version=${API_NETWORK}"

SUBNET_ID="/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}/subnets/${SUBNET}"

echo "-- PUT network interface (asigna IP privada automáticamente) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/networkInterfaces/${NIC}?api-version=${API_NETWORK}" \
  --body "{\"location\": \"${LOCATION}\", \"properties\": {\"ipConfigurations\": [{\"name\": \"ipconfig1\", \"properties\": {\"subnet\": {\"id\": \"${SUBNET_ID}\"}}}]}}"

echo "-- GET network interface --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/networkInterfaces/${NIC}?api-version=${API_NETWORK}"

NIC_ID="/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/networkInterfaces/${NIC}"

echo "-- PUT managed disk --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/disks/${DISK}?api-version=${API_COMPUTE}" \
  --body "{\"location\": \"${LOCATION}\", \"sku\": {\"name\": \"Standard_LRS\"}, \"properties\": {\"diskSizeGB\": 32, \"creationData\": {\"createOption\": \"Empty\"}}}"

echo "-- GET managed disk --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/disks/${DISK}?api-version=${API_COMPUTE}"

echo "-- LIST imágenes del catálogo estático (Canonical Ubuntu 22.04) --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/providers/Microsoft.Compute/locations/${LOCATION}/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus/22_04-lts-gen2/versions?api-version=${API_COMPUTE_IMAGES}"

echo "-- PUT virtual machine (async, 202 con cuerpo) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/virtualMachines/${VM}?api-version=${API_COMPUTE}" \
  --body "{\"location\": \"${LOCATION}\", \"properties\": {\"hardwareProfile\": {\"vmSize\": \"Standard_B1s\"}, \"storageProfile\": {\"imageReference\": {\"publisher\": \"Canonical\", \"offer\": \"0001-com-ubuntu-server-jammy\", \"sku\": \"22_04-lts-gen2\", \"version\": \"latest\"}}, \"osProfile\": {\"computerName\": \"smoketestvm\", \"adminUsername\": \"azureuser\", \"adminPassword\": \"P@ssw0rd1234!\"}, \"networkProfile\": {\"networkInterfaces\": [{\"id\": \"${NIC_ID}\"}]}}}"

echo "-- GET virtual machine (la respuesta no debe incluir adminPassword) --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/virtualMachines/${VM}?api-version=${API_COMPUTE}"

echo "-- POST powerOff virtual machine (async, 202 sin cuerpo) --"
az rest --method post \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/virtualMachines/${VM}/powerOff?api-version=${API_COMPUTE}"

echo "-- POST start virtual machine (async, 202 sin cuerpo) --"
az rest --method post \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/virtualMachines/${VM}/start?api-version=${API_COMPUTE}"

echo "-- DELETE virtual machine (async, 202) --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/virtualMachines/${VM}?api-version=${API_COMPUTE}"

echo "-- DELETE managed disk --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Compute/disks/${DISK}?api-version=${API_COMPUTE}"

echo "-- DELETE network interface --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/networkInterfaces/${NIC}?api-version=${API_NETWORK}"

echo "-- DELETE subnet --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}/subnets/${SUBNET}?api-version=${API_NETWORK}"

echo "-- DELETE virtual network --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Network/virtualNetworks/${VNET}?api-version=${API_NETWORK}"

API_KEYVAULT="2023-07-01"
VAULT="smoketestkv"
TENANT_ID="00000000-0000-0000-0000-000000000000"

echo "-- PUT key vault (ARM, sync) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.KeyVault/vaults/${VAULT}?api-version=${API_KEYVAULT}" \
  --body "{\"location\": \"${LOCATION}\", \"properties\": {\"sku\": {\"family\": \"A\", \"name\": \"standard\"}, \"tenantId\": \"${TENANT_ID}\"}}"

echo "-- GET key vault --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.KeyVault/vaults/${VAULT}?api-version=${API_KEYVAULT}"

echo "-- LIST key vaults --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.KeyVault/vaults?api-version=${API_KEYVAULT}"

echo "-- PUT secret (data plane) --"
az rest --method put \
  --url "${ENDPOINT}/${VAULT}.vault/secrets/smoketest-secret" \
  --body "{\"value\": \"super-secreto\"}"

echo "-- GET secret --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/secrets/smoketest-secret"

echo "-- LIST secrets (sin 'value') --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/secrets"

echo "-- DELETE secret --"
az rest --method delete \
  --url "${ENDPOINT}/${VAULT}.vault/secrets/smoketest-secret"

echo "-- PUT key (data plane, material simulado) --"
az rest --method put \
  --url "${ENDPOINT}/${VAULT}.vault/keys/smoketest-key" \
  --body "{\"kty\": \"RSA\"}"

echo "-- GET key --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/keys/smoketest-key"

echo "-- LIST keys --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/keys"

echo "-- DELETE key --"
az rest --method delete \
  --url "${ENDPOINT}/${VAULT}.vault/keys/smoketest-key"

echo "-- PUT certificate (data plane, material simulado) --"
az rest --method put \
  --url "${ENDPOINT}/${VAULT}.vault/certificates/smoketest-cert" \
  --body "{\"policy\": {}}"

echo "-- GET certificate --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/certificates/smoketest-cert"

echo "-- LIST certificates --"
az rest --method get \
  --url "${ENDPOINT}/${VAULT}.vault/certificates"

echo "-- DELETE certificate --"
az rest --method delete \
  --url "${ENDPOINT}/${VAULT}.vault/certificates/smoketest-cert"

echo "-- DELETE key vault --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.KeyVault/vaults/${VAULT}?api-version=${API_KEYVAULT}"

API_SERVICEBUS="2021-11-01"
NAMESPACE="smoketestns"
QUEUE_SB="smoketest-sbqueue"
TOPIC="smoketest-topic"
SUBSCRIPTION="smoketest-sub"

echo "-- PUT Service Bus namespace (async, 202) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}?api-version=${API_SERVICEBUS}" \
  --body "{\"location\": \"${LOCATION}\"}"

echo "-- GET Service Bus namespace --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}?api-version=${API_SERVICEBUS}"

echo "-- PUT Service Bus queue --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/queues/${QUEUE_SB}?api-version=${API_SERVICEBUS}" \
  --body "{\"properties\": {}}"

echo "-- SEND mensaje a la cola (data plane) --"
az rest --method post \
  --url "${ENDPOINT}/${NAMESPACE}.servicebus/${QUEUE_SB}/messages" \
  --body "{\"body\": \"hola desde Service Bus\"}"

echo "-- RECEIVE mensaje (peek-lock) --"
SB_RESPONSE=$(az rest --method get \
  --url "${ENDPOINT}/${NAMESPACE}.servicebus/${QUEUE_SB}/messages?peeklock=true")
echo "${SB_RESPONSE}"
SB_MESSAGE_ID=$(echo "${SB_RESPONSE}" | sed -n 's/.*"messageId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
SB_LOCK_TOKEN=$(echo "${SB_RESPONSE}" | sed -n 's/.*"lockToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)

echo "-- COMPLETE mensaje (requiere lockToken de la última recepción) --"
az rest --method delete \
  --url "${ENDPOINT}/${NAMESPACE}.servicebus/${QUEUE_SB}/messages/${SB_MESSAGE_ID}?lockToken=${SB_LOCK_TOKEN}"

echo "-- DELETE Service Bus queue --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/queues/${QUEUE_SB}?api-version=${API_SERVICEBUS}"

echo "-- PUT Service Bus topic --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/topics/${TOPIC}?api-version=${API_SERVICEBUS}" \
  --body "{\"properties\": {}}"

echo "-- PUT Service Bus subscription --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/topics/${TOPIC}/subscriptions/${SUBSCRIPTION}?api-version=${API_SERVICEBUS}" \
  --body "{\"properties\": {}}"

echo "-- SEND mensaje al topic (fan-out a sus subscriptions) --"
az rest --method post \
  --url "${ENDPOINT}/${NAMESPACE}.servicebus/${TOPIC}/messages" \
  --body "{\"body\": \"hola desde el topic\"}"

echo "-- RECEIVE mensaje desde la subscription --"
az rest --method get \
  --url "${ENDPOINT}/${NAMESPACE}.servicebus/${TOPIC}/subscriptions/${SUBSCRIPTION}/messages?peeklock=true"

echo "-- DELETE Service Bus subscription --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/topics/${TOPIC}/subscriptions/${SUBSCRIPTION}?api-version=${API_SERVICEBUS}"

echo "-- DELETE Service Bus topic --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}/topics/${TOPIC}?api-version=${API_SERVICEBUS}"

echo "-- DELETE Service Bus namespace --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.ServiceBus/namespaces/${NAMESPACE}?api-version=${API_SERVICEBUS}"

API_COSMOSDB="2023-04-15"
COSMOS_ACCOUNT="smoketestcosmos"
COSMOS_DB="smoketestdb"
COSMOS_CONTAINER="smoketestcontainer"

echo "-- PUT cuenta de Cosmos DB (async, 202) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}?api-version=${API_COSMOSDB}" \
  --body "{\"location\": \"${LOCATION}\"}"

echo "-- GET cuenta de Cosmos DB --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}?api-version=${API_COSMOSDB}"

echo "-- PUT base de datos SQL --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}/sqlDatabases/${COSMOS_DB}?api-version=${API_COSMOSDB}" \
  --body "{\"properties\": {\"resource\": {\"id\": \"${COSMOS_DB}\"}}}"

echo "-- PUT container (requiere partitionKey) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}/sqlDatabases/${COSMOS_DB}/containers/${COSMOS_CONTAINER}?api-version=${API_COSMOSDB}" \
  --body "{\"properties\": {\"resource\": {\"id\": \"${COSMOS_CONTAINER}\", \"partitionKey\": {\"paths\": [\"/pk\"]}}}}"

echo "-- PUT documento (data plane) --"
az rest --method put \
  --url "${ENDPOINT}/${COSMOS_ACCOUNT}.documents/dbs/${COSMOS_DB}/colls/${COSMOS_CONTAINER}/docs/smoketest-doc" \
  --body "{\"pk\": \"x\", \"value\": 42}"

echo "-- GET documento --"
az rest --method get \
  --url "${ENDPOINT}/${COSMOS_ACCOUNT}.documents/dbs/${COSMOS_DB}/colls/${COSMOS_CONTAINER}/docs/smoketest-doc"

echo "-- LIST documentos del container --"
az rest --method get \
  --url "${ENDPOINT}/${COSMOS_ACCOUNT}.documents/dbs/${COSMOS_DB}/colls/${COSMOS_CONTAINER}/docs"

echo "-- DELETE documento --"
az rest --method delete \
  --url "${ENDPOINT}/${COSMOS_ACCOUNT}.documents/dbs/${COSMOS_DB}/colls/${COSMOS_CONTAINER}/docs/smoketest-doc"

echo "-- DELETE container --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}/sqlDatabases/${COSMOS_DB}/containers/${COSMOS_CONTAINER}?api-version=${API_COSMOSDB}"

echo "-- DELETE base de datos SQL --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}/sqlDatabases/${COSMOS_DB}?api-version=${API_COSMOSDB}"

echo "-- DELETE cuenta de Cosmos DB --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.DocumentDB/databaseAccounts/${COSMOS_ACCOUNT}?api-version=${API_COSMOSDB}"

API_MONITOR="2022-10-01"
API_INSIGHTS="2021-08-01"
WORKSPACE="smoketestworkspace"
ACTION_GROUP="smoketestactiongroup"
METRIC_ALERT="smoketestmetricalert"

echo "-- PUT Log Analytics workspace (ARM, sync) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.OperationalInsights/workspaces/${WORKSPACE}?api-version=${API_MONITOR}" \
  --body "{\"location\": \"${LOCATION}\"}"

echo "-- GET Log Analytics workspace --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.OperationalInsights/workspaces/${WORKSPACE}?api-version=${API_MONITOR}"

echo "-- LIST Log Analytics workspaces --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.OperationalInsights/workspaces?api-version=${API_MONITOR}"

echo "-- POST sharedKeys (azurerm_log_analytics_workspace primary/secondary_shared_key) --"
az rest --method post \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.OperationalInsights/workspaces/${WORKSPACE}/sharedKeys?api-version=${API_MONITOR}"

echo "-- POST Log Analytics query (data plane, stub: siempre vacío) --"
az rest --method post \
  --url "${ENDPOINT}/v1/workspaces/smoketest-fake-customer-id/query" \
  --body "{\"query\": \"AzureActivity | take 1\"}"

echo "-- PUT action group (ARM, sync) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/actionGroups/${ACTION_GROUP}?api-version=${API_INSIGHTS}" \
  --body "{\"location\": \"global\", \"properties\": {\"groupShortName\": \"smoketest\", \"emailReceivers\": [{\"name\": \"admin\", \"emailAddress\": \"admin@example.com\"}]}}"

echo "-- GET action group --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/actionGroups/${ACTION_GROUP}?api-version=${API_INSIGHTS}"

echo "-- LIST action groups --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/actionGroups?api-version=${API_INSIGHTS}"

ACTION_GROUP_ID="/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/actionGroups/${ACTION_GROUP}"

echo "-- PUT metric alert (ARM, sync) --"
az rest --method put \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/metricAlerts/${METRIC_ALERT}?api-version=${API_INSIGHTS}" \
  --body "{\"location\": \"global\", \"properties\": {\"severity\": 2, \"scopes\": [\"${NIC_ID}\"], \"criteria\": {\"odata.type\": \"Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria\"}, \"actions\": [{\"actionGroupId\": \"${ACTION_GROUP_ID}\"}]}}"

echo "-- GET metric alert --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/metricAlerts/${METRIC_ALERT}?api-version=${API_INSIGHTS}"

echo "-- LIST metric alerts --"
az rest --method get \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/metricAlerts?api-version=${API_INSIGHTS}"

echo "-- DELETE metric alert --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/metricAlerts/${METRIC_ALERT}?api-version=${API_INSIGHTS}"

echo "-- DELETE action group --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.Insights/actionGroups/${ACTION_GROUP}?api-version=${API_INSIGHTS}"

echo "-- DELETE Log Analytics workspace --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}/providers/Microsoft.OperationalInsights/workspaces/${WORKSPACE}?api-version=${API_MONITOR}"

echo "-- DELETE resource group (async, 202) --"
az rest --method delete \
  --url "${ENDPOINT}/subscriptions/${SUB}/resourceGroups/${RG}?api-version=${API_RG}"

echo "== Listo =="
