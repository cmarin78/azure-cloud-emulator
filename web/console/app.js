// Azure Emulator console — vanilla JS, no build step, talks directly to
// the emulator's own JSON REST API via fetch (same origin).

function sub() {
  return document.getElementById("subInput").value.trim();
}
function rg() {
  return document.getElementById("rgInput").value.trim();
}

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (res.status === 204) return null;
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const msg = (data && data.error && data.error.message) || res.statusText;
    throw new Error(msg);
  }
  return data;
}

// ---- nav ----
document.querySelectorAll(".nav-item").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".nav-item").forEach((b) => b.classList.remove("active"));
    document.querySelectorAll(".view").forEach((v) => v.classList.remove("active"));
    btn.classList.add("active");
    document.getElementById("view-" + btn.dataset.view).classList.add("active");
  });
});

function refreshAll() {
  loadResourceGroups();
  loadStorageAccounts();
  loadVMs();
  loadVaults();
  loadNamespaces();
  loadCosmosAccounts();
}
document.getElementById("subInput").addEventListener("change", refreshAll);
document.getElementById("rgInput").addEventListener("change", refreshAll);

// ---- Resource Groups (Microsoft.Resources, api-version 2021-04-01) ----
const API_RG = "2021-04-01";

async function loadResourceGroups() {
  const data = await api("GET", `/subscriptions/${sub()}/resourceGroups?api-version=${API_RG}`);
  const rows = (data.value || [])
    .map(
      (g) => `<tr>
        <td>${g.name}</td>
        <td>${g.location}</td>
        <td>${g.properties?.provisioningState ?? ""}</td>
        <td><button class="link" onclick="deleteResourceGroup('${g.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("rgTable").innerHTML = rows;
}

async function createResourceGroup() {
  const name = document.getElementById("newRgName").value.trim();
  const location = document.getElementById("newRgLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `/subscriptions/${sub()}/resourceGroups/${name}?api-version=${API_RG}`, { location });
  document.getElementById("newRgName").value = "";
  loadResourceGroups();
}

async function deleteResourceGroup(name) {
  await api("DELETE", `/subscriptions/${sub()}/resourceGroups/${name}?api-version=${API_RG}`);
  loadResourceGroups();
}

// ---- Storage Accounts (Microsoft.Storage, api-version 2023-01-01) ----
const API_STORAGE = "2023-01-01";

function storageBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Storage/storageAccounts`;
}

async function loadStorageAccounts() {
  const data = await api("GET", `${storageBase()}?api-version=${API_STORAGE}`);
  const rows = (data.value || [])
    .map(
      (a) => `<tr>
        <td>${a.name}</td>
        <td>${a.location}</td>
        <td>${a.kind ?? ""}</td>
        <td>${a.properties?.provisioningState ?? ""}</td>
        <td>${a.properties?.primaryEndpoints?.blob ?? ""}</td>
        <td><button class="link" onclick="deleteStorageAccount('${a.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("storageTable").innerHTML = rows;
}

async function createStorageAccount() {
  const name = document.getElementById("newStorageName").value.trim();
  const location = document.getElementById("newStorageLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${storageBase()}/${name}?api-version=${API_STORAGE}`, {
    location,
    sku: { name: "Standard_LRS" },
    kind: "StorageV2",
  });
  document.getElementById("newStorageName").value = "";
  loadStorageAccounts();
}

async function deleteStorageAccount(name) {
  await api("DELETE", `${storageBase()}/${name}?api-version=${API_STORAGE}`);
  loadStorageAccounts();
}

// ---- Virtual Machines (Microsoft.Compute, api-version 2023-09-01) ----
const API_COMPUTE = "2023-09-01";

function vmBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Compute/virtualMachines`;
}

async function loadVMs() {
  const data = await api("GET", `${vmBase()}?api-version=${API_COMPUTE}`);
  const rows = (data.value || [])
    .map((vm) => {
      const power =
        (vm.properties?.instanceView?.statuses || []).find((s) => s.code?.startsWith("PowerState/"))
          ?.displayStatus ?? "";
      return `<tr>
        <td>${vm.name}</td>
        <td>${vm.location}</td>
        <td>${vm.properties?.hardwareProfile?.vmSize ?? ""}</td>
        <td>${vm.properties?.provisioningState ?? ""}</td>
        <td>${power}</td>
        <td>
          <button class="link" onclick="startVM('${vm.name}')">Start</button>
          <button class="link" onclick="stopVM('${vm.name}')">Stop</button>
          <button class="link" onclick="deleteVM('${vm.name}')">Borrar</button>
        </td>
      </tr>`;
    })
    .join("");
  document.getElementById("vmTable").innerHTML = rows;
}

async function startVM(name) {
  await api("POST", `${vmBase()}/${name}/start?api-version=${API_COMPUTE}`);
  loadVMs();
}
async function stopVM(name) {
  await api("POST", `${vmBase()}/${name}/powerOff?api-version=${API_COMPUTE}`);
  loadVMs();
}
async function deleteVM(name) {
  await api("DELETE", `${vmBase()}/${name}?api-version=${API_COMPUTE}`);
  loadVMs();
}

// ---- Key Vault (Microsoft.KeyVault, api-version 2023-07-01) ----
const API_KEYVAULT = "2023-07-01";

function vaultBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.KeyVault/vaults`;
}

async function loadVaults() {
  const data = await api("GET", `${vaultBase()}?api-version=${API_KEYVAULT}`);
  const rows = (data.value || [])
    .map(
      (v) => `<tr>
        <td>${v.name}</td>
        <td>${v.location}</td>
        <td>${v.properties?.provisioningState ?? ""}</td>
        <td>${v.properties?.vaultUri ?? ""}</td>
        <td><button class="link" onclick="deleteVault('${v.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("kvTable").innerHTML = rows;
}

async function createVault() {
  const name = document.getElementById("newVaultName").value.trim();
  const location = document.getElementById("newVaultLocation").value.trim();
  const tenantId = document.getElementById("newVaultTenant").value.trim();
  if (!name || !location || !tenantId) return;
  await api("PUT", `${vaultBase()}/${name}?api-version=${API_KEYVAULT}`, {
    location,
    properties: {
      sku: { family: "A", name: "standard" },
      tenantId,
      accessPolicies: [],
    },
  });
  document.getElementById("newVaultName").value = "";
  loadVaults();
}

async function deleteVault(name) {
  await api("DELETE", `${vaultBase()}/${name}?api-version=${API_KEYVAULT}`);
  loadVaults();
}

// ---- Service Bus (Microsoft.ServiceBus, api-version 2021-11-01) ----
const API_SERVICEBUS = "2021-11-01";

function namespaceBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ServiceBus/namespaces`;
}

async function loadNamespaces() {
  const data = await api("GET", `${namespaceBase()}?api-version=${API_SERVICEBUS}`);
  const rows = (data.value || [])
    .map(
      (n) => `<tr>
        <td>${n.name}</td>
        <td>${n.location}</td>
        <td>${n.properties?.provisioningState ?? ""}</td>
        <td>${n.properties?.serviceBusEndpoint ?? ""}</td>
        <td><button class="link" onclick="deleteNamespace('${n.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("sbTable").innerHTML = rows;
}

async function createNamespace() {
  const name = document.getElementById("newSbName").value.trim();
  const location = document.getElementById("newSbLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${namespaceBase()}/${name}?api-version=${API_SERVICEBUS}`, {
    location,
    sku: { name: "Standard" },
  });
  document.getElementById("newSbName").value = "";
  loadNamespaces();
}

async function deleteNamespace(name) {
  await api("DELETE", `${namespaceBase()}/${name}?api-version=${API_SERVICEBUS}`);
  loadNamespaces();
}

// ---- Cosmos DB (Microsoft.DocumentDB, api-version 2023-04-15) ----
const API_COSMOSDB = "2023-04-15";

function cosmosBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.DocumentDB/databaseAccounts`;
}

async function loadCosmosAccounts() {
  const data = await api("GET", `${cosmosBase()}?api-version=${API_COSMOSDB}`);
  const rows = (data.value || [])
    .map(
      (a) => `<tr>
        <td>${a.name}</td>
        <td>${a.location}</td>
        <td>${a.properties?.provisioningState ?? ""}</td>
        <td>${a.properties?.documentEndpoint ?? ""}</td>
        <td><button class="link" onclick="deleteCosmosAccount('${a.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("cosmosTable").innerHTML = rows;
}

async function createCosmosAccount() {
  const name = document.getElementById("newCosmosName").value.trim();
  const location = document.getElementById("newCosmosLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${cosmosBase()}/${name}?api-version=${API_COSMOSDB}`, {
    location,
    kind: "GlobalDocumentDB",
    properties: { locations: [{ locationName: location }] },
  });
  document.getElementById("newCosmosName").value = "";
  loadCosmosAccounts();
}

async function deleteCosmosAccount(name) {
  await api("DELETE", `${cosmosBase()}/${name}?api-version=${API_COSMOSDB}`);
  loadCosmosAccounts();
}

// ---- initial load ----
refreshAll();
