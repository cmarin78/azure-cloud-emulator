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
  loadWorkspaces();
  loadActionGroups();
  loadMetricAlerts();
  loadPlans();
  loadSites();
  loadVNets();
  loadNSGs();
  loadPublicIPs();
  loadLoadBalancers();
  loadRouteTables();
  loadDNSZones();
  loadClusters();
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

// ---- Monitor: Log Analytics workspaces (Microsoft.OperationalInsights, Phase 10) ----
const API_MONITOR = "2022-10-01";

function monitorBase(provider, kind) {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/${provider}/${kind}`;
}

async function loadWorkspaces() {
  const data = await api("GET", `${monitorBase("Microsoft.OperationalInsights", "workspaces")}?api-version=${API_MONITOR}`);
  const rows = (data.value || [])
    .map(
      (w) => `<tr>
        <td>${w.name}</td>
        <td>${w.location}</td>
        <td>${w.properties?.provisioningState ?? ""}</td>
        <td>${w.properties?.customerId ?? ""}</td>
        <td><button class="link" onclick="deleteWorkspace('${w.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("workspaceTable").innerHTML = rows;
}

async function createWorkspace() {
  const name = document.getElementById("newWorkspaceName").value.trim();
  const location = document.getElementById("newWorkspaceLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${monitorBase("Microsoft.OperationalInsights", "workspaces")}/${name}?api-version=${API_MONITOR}`, { location });
  document.getElementById("newWorkspaceName").value = "";
  loadWorkspaces();
}

async function deleteWorkspace(name) {
  await api("DELETE", `${monitorBase("Microsoft.OperationalInsights", "workspaces")}/${name}?api-version=${API_MONITOR}`);
  loadWorkspaces();
}

// ---- Monitor: Action Groups (Microsoft.Insights, Phase 10) ----
async function loadActionGroups() {
  const data = await api("GET", `${monitorBase("Microsoft.Insights", "actionGroups")}?api-version=${API_MONITOR}`);
  const rows = (data.value || [])
    .map(
      (g) => `<tr>
        <td>${g.name}</td>
        <td>${g.properties?.groupShortName ?? ""}</td>
        <td>${g.properties?.lastNotificationStatus ?? ""}</td>
        <td><button class="link" onclick="deleteActionGroup('${g.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("agTable").innerHTML = rows;
}

async function createActionGroup() {
  const name = document.getElementById("newAgName").value.trim();
  const shortName = document.getElementById("newAgShortName").value.trim();
  const email = document.getElementById("newAgEmail").value.trim();
  if (!name || !shortName) return;
  const properties = { groupShortName: shortName };
  if (email) properties.emailReceivers = [{ name: "admin", emailAddress: email }];
  await api("PUT", `${monitorBase("Microsoft.Insights", "actionGroups")}/${name}?api-version=${API_MONITOR}`, {
    location: "global",
    properties,
  });
  document.getElementById("newAgName").value = "";
  document.getElementById("newAgShortName").value = "";
  document.getElementById("newAgEmail").value = "";
  loadActionGroups();
}

async function deleteActionGroup(name) {
  await api("DELETE", `${monitorBase("Microsoft.Insights", "actionGroups")}/${name}?api-version=${API_MONITOR}`);
  loadActionGroups();
}

// ---- Monitor: Metric Alerts (Microsoft.Insights, Phase 10) ----
async function loadMetricAlerts() {
  const data = await api("GET", `${monitorBase("Microsoft.Insights", "metricAlerts")}?api-version=${API_MONITOR}`);
  const rows = (data.value || [])
    .map(
      (a) => `<tr>
        <td>${a.name}</td>
        <td>${a.properties?.severity ?? ""}</td>
        <td>${a.properties?.enabled ? "sí" : "no"}</td>
        <td>${(a.properties?.scopes || []).join(", ")}</td>
        <td><button class="link" onclick="deleteMetricAlert('${a.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("maTable").innerHTML = rows;
}

async function createMetricAlert() {
  const name = document.getElementById("newMaName").value.trim();
  const scope = document.getElementById("newMaScope").value.trim();
  if (!name || !scope) return;
  await api("PUT", `${monitorBase("Microsoft.Insights", "metricAlerts")}/${name}?api-version=${API_MONITOR}`, {
    location: "global",
    properties: {
      scopes: [scope],
      criteria: { "odata.type": "Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria" },
    },
  });
  document.getElementById("newMaName").value = "";
  document.getElementById("newMaScope").value = "";
  loadMetricAlerts();
}

async function deleteMetricAlert(name) {
  await api("DELETE", `${monitorBase("Microsoft.Insights", "metricAlerts")}/${name}?api-version=${API_MONITOR}`);
  loadMetricAlerts();
}

// ---- App Service: Plans + Sites (Microsoft.Web, Phase 11) ----
const API_APPSERVICE = "2022-03-01";

function appserviceBase(kind) {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/${kind}`;
}

async function loadPlans() {
  const data = await api("GET", `${appserviceBase("serverfarms")}?api-version=${API_APPSERVICE}`);
  const rows = (data.value || [])
    .map(
      (p) => `<tr>
        <td>${p.name}</td>
        <td>${p.location}</td>
        <td>${p.sku?.name ?? ""}</td>
        <td>${p.properties?.provisioningState ?? ""}</td>
        <td><button class="link" onclick="deletePlan('${p.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("planTable").innerHTML = rows;
}

async function createPlan() {
  const name = document.getElementById("newPlanName").value.trim();
  const location = document.getElementById("newPlanLocation").value.trim();
  const sku = document.getElementById("newPlanSku").value.trim() || "B1";
  if (!name || !location) return;
  await api("PUT", `${appserviceBase("serverfarms")}/${name}?api-version=${API_APPSERVICE}`, {
    location,
    sku: { name: sku },
  });
  document.getElementById("newPlanName").value = "";
  loadPlans();
}

async function deletePlan(name) {
  await api("DELETE", `${appserviceBase("serverfarms")}/${name}?api-version=${API_APPSERVICE}`);
  loadPlans();
}

async function loadSites() {
  const data = await api("GET", `${appserviceBase("sites")}?api-version=${API_APPSERVICE}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.properties?.provisioningState ?? ""}</td>
        <td>${s.properties?.state ?? ""}</td>
        <td>${s.properties?.defaultHostName ?? ""}</td>
        <td>
          <button class="link" onclick="startSite('${s.name}')">Start</button>
          <button class="link" onclick="stopSite('${s.name}')">Stop</button>
          <button class="link" onclick="restartSite('${s.name}')">Restart</button>
          <button class="link" onclick="deleteSite('${s.name}')">Borrar</button>
        </td>
      </tr>`
    )
    .join("");
  document.getElementById("siteTable").innerHTML = rows;
}

async function createSite() {
  const name = document.getElementById("newSiteName").value.trim();
  const location = document.getElementById("newSiteLocation").value.trim();
  const planName = document.getElementById("newSitePlan").value.trim();
  if (!name || !location || !planName) return;
  const serverFarmId = `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/serverfarms/${planName}`;
  await api("PUT", `${appserviceBase("sites")}/${name}?api-version=${API_APPSERVICE}`, {
    location,
    properties: { serverFarmId },
  });
  document.getElementById("newSiteName").value = "";
  loadSites();
}

async function startSite(name) {
  await api("POST", `${appserviceBase("sites")}/${name}/start?api-version=${API_APPSERVICE}`);
  loadSites();
}
async function stopSite(name) {
  await api("POST", `${appserviceBase("sites")}/${name}/stop?api-version=${API_APPSERVICE}`);
  loadSites();
}
async function restartSite(name) {
  await api("POST", `${appserviceBase("sites")}/${name}/restart?api-version=${API_APPSERVICE}`);
  loadSites();
}
async function deleteSite(name) {
  await api("DELETE", `${appserviceBase("sites")}/${name}?api-version=${API_APPSERVICE}`);
  loadSites();
}

// ---- Networking (Microsoft.Network, Phase 12) ----
const API_NETWORK = "2023-09-01";

function networkBase(kind) {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Network/${kind}`;
}

async function loadVNets() {
  const data = await api("GET", `${networkBase("virtualNetworks")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (v) => `<tr>
        <td>${v.name}</td>
        <td>${v.location}</td>
        <td>${(v.properties?.addressSpace?.addressPrefixes || []).join(", ")}</td>
        <td><button class="link" onclick="deleteVNet('${v.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("vnetTable").innerHTML = rows;
}

async function createVNet() {
  const name = document.getElementById("newVNetName").value.trim();
  const location = document.getElementById("newVNetLocation").value.trim();
  const prefix = document.getElementById("newVNetPrefix").value.trim() || "10.0.0.0/16";
  if (!name || !location) return;
  await api("PUT", `${networkBase("virtualNetworks")}/${name}?api-version=${API_NETWORK}`, {
    location,
    properties: { addressSpace: { addressPrefixes: [prefix] } },
  });
  document.getElementById("newVNetName").value = "";
  loadVNets();
}

async function deleteVNet(name) {
  await api("DELETE", `${networkBase("virtualNetworks")}/${name}?api-version=${API_NETWORK}`);
  loadVNets();
}

async function loadNSGs() {
  const data = await api("GET", `${networkBase("networkSecurityGroups")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (n) => `<tr>
        <td>${n.name}</td>
        <td>${n.location}</td>
        <td>${(n.properties?.securityRules || []).length}</td>
        <td><button class="link" onclick="deleteNSG('${n.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("nsgTable").innerHTML = rows;
}

async function createNSG() {
  const name = document.getElementById("newNsgName").value.trim();
  const location = document.getElementById("newNsgLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${networkBase("networkSecurityGroups")}/${name}?api-version=${API_NETWORK}`, { location });
  document.getElementById("newNsgName").value = "";
  loadNSGs();
}

async function deleteNSG(name) {
  await api("DELETE", `${networkBase("networkSecurityGroups")}/${name}?api-version=${API_NETWORK}`);
  loadNSGs();
}

async function loadPublicIPs() {
  const data = await api("GET", `${networkBase("publicIPAddresses")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (p) => `<tr>
        <td>${p.name}</td>
        <td>${p.location}</td>
        <td>${p.properties?.publicIPAllocationMethod ?? ""}</td>
        <td>${p.properties?.ipAddress ?? ""}</td>
        <td><button class="link" onclick="deletePublicIP('${p.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("pipTable").innerHTML = rows;
}

async function createPublicIP() {
  const name = document.getElementById("newPipName").value.trim();
  const location = document.getElementById("newPipLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${networkBase("publicIPAddresses")}/${name}?api-version=${API_NETWORK}`, { location });
  document.getElementById("newPipName").value = "";
  loadPublicIPs();
}

async function deletePublicIP(name) {
  await api("DELETE", `${networkBase("publicIPAddresses")}/${name}?api-version=${API_NETWORK}`);
  loadPublicIPs();
}

async function loadLoadBalancers() {
  const data = await api("GET", `${networkBase("loadBalancers")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (l) => `<tr>
        <td>${l.name}</td>
        <td>${l.location}</td>
        <td>${(l.properties?.frontendIPConfigurations || []).length}</td>
        <td>${(l.properties?.backendAddressPools || []).length}</td>
        <td><button class="link" onclick="deleteLoadBalancer('${l.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("lbTable").innerHTML = rows;
}

async function createLoadBalancer() {
  const name = document.getElementById("newLbName").value.trim();
  const location = document.getElementById("newLbLocation").value.trim();
  const pipName = document.getElementById("newLbPip").value.trim();
  if (!name || !location || !pipName) return;
  const pipID = `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Network/publicIPAddresses/${pipName}`;
  await api("PUT", `${networkBase("loadBalancers")}/${name}?api-version=${API_NETWORK}`, {
    location,
    properties: {
      frontendIPConfigurations: [{ name: "frontend1", properties: { publicIPAddress: { id: pipID } } }],
      backendAddressPools: [{ name: "backend1" }],
    },
  });
  document.getElementById("newLbName").value = "";
  loadLoadBalancers();
}

async function deleteLoadBalancer(name) {
  await api("DELETE", `${networkBase("loadBalancers")}/${name}?api-version=${API_NETWORK}`);
  loadLoadBalancers();
}

async function loadRouteTables() {
  const data = await api("GET", `${networkBase("routeTables")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (r) => `<tr>
        <td>${r.name}</td>
        <td>${r.location}</td>
        <td>${(r.properties?.routes || []).length}</td>
        <td><button class="link" onclick="deleteRouteTable('${r.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("rtTable").innerHTML = rows;
}

async function createRouteTable() {
  const name = document.getElementById("newRtName").value.trim();
  const location = document.getElementById("newRtLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${networkBase("routeTables")}/${name}?api-version=${API_NETWORK}`, { location });
  document.getElementById("newRtName").value = "";
  loadRouteTables();
}

async function deleteRouteTable(name) {
  await api("DELETE", `${networkBase("routeTables")}/${name}?api-version=${API_NETWORK}`);
  loadRouteTables();
}

async function loadDNSZones() {
  const data = await api("GET", `${networkBase("privateDnsZones")}?api-version=${API_NETWORK}`);
  const rows = (data.value || [])
    .map(
      (z) => `<tr>
        <td>${z.name}</td>
        <td>${z.properties?.numberOfRecordSets ?? 0}</td>
        <td><button class="link" onclick="deleteDNSZone('${z.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("dnsTable").innerHTML = rows;
}

async function createDNSZone() {
  const name = document.getElementById("newDnsName").value.trim();
  if (!name) return;
  await api("PUT", `${networkBase("privateDnsZones")}/${name}?api-version=${API_NETWORK}`, {});
  document.getElementById("newDnsName").value = "";
  loadDNSZones();
}

async function deleteDNSZone(name) {
  await api("DELETE", `${networkBase("privateDnsZones")}/${name}?api-version=${API_NETWORK}`);
  loadDNSZones();
}

// ---- AKS: Managed Clusters (Microsoft.ContainerService, Phase 13) ----
const API_AKS = "2023-10-01";

function aksBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ContainerService/managedClusters`;
}

async function loadClusters() {
  const data = await api("GET", `${aksBase()}?api-version=${API_AKS}`);
  const rows = (data.value || [])
    .map(
      (c) => `<tr>
        <td>${c.name}</td>
        <td>${c.location}</td>
        <td>${c.properties?.provisioningState ?? ""}</td>
        <td>${c.properties?.dnsPrefix ?? ""}</td>
        <td>${(c.properties?.agentPoolProfiles || []).length}</td>
        <td>
          <button class="link" onclick="viewClusterCredentials('${c.name}')">Ver credenciales</button>
          <button class="link" onclick="deleteCluster('${c.name}')">Borrar</button>
        </td>
      </tr>`
    )
    .join("");
  document.getElementById("aksTable").innerHTML = rows;
}

async function createCluster() {
  const name = document.getElementById("newAksName").value.trim();
  const location = document.getElementById("newAksLocation").value.trim();
  const dnsPrefix = document.getElementById("newAksDnsPrefix").value.trim();
  if (!name || !location || !dnsPrefix) return;
  await api("PUT", `${aksBase()}/${name}?api-version=${API_AKS}`, {
    location,
    properties: { dnsPrefix },
  });
  document.getElementById("newAksName").value = "";
  document.getElementById("newAksDnsPrefix").value = "";
  loadClusters();
}

async function deleteCluster(name) {
  await api("DELETE", `${aksBase()}/${name}?api-version=${API_AKS}`);
  loadClusters();
}

async function viewClusterCredentials(name) {
  const data = await api("POST", `${aksBase()}/${name}/listClusterUserCredential?api-version=${API_AKS}`);
  alert(JSON.stringify(data, null, 2));
}

// ---- Functions: function definitions (Microsoft.Web/sites/functions, Phase 14) ----
const API_FUNCTIONS = "2022-03-01";

function functionsBase() {
  const app = document.getElementById("funcAppInput").value.trim();
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/sites/${app}/functions`;
}

async function loadFunctions() {
  const app = document.getElementById("funcAppInput").value.trim();
  if (!app) {
    document.getElementById("funcTable").innerHTML = "";
    return;
  }
  const data = await api("GET", `${functionsBase()}?api-version=${API_FUNCTIONS}`);
  const rows = (data.value || [])
    .map(
      (f) => `<tr>
        <td>${f.properties?.name ?? f.name}</td>
        <td>${f.properties?.language ?? ""}</td>
        <td>${f.properties?.invoke_url_template ?? ""}</td>
        <td><button class="link" onclick="deleteFunction('${f.properties?.name ?? f.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("funcTable").innerHTML = rows;
}

async function createFunction() {
  const app = document.getElementById("funcAppInput").value.trim();
  const name = document.getElementById("newFuncName").value.trim();
  const language = document.getElementById("newFuncLanguage").value.trim() || "javascript";
  if (!app || !name) return;
  await api("PUT", `${functionsBase()}/${name}?api-version=${API_FUNCTIONS}`, {
    properties: { language, config: { bindings: [{ type: "httpTrigger", direction: "in", authLevel: "function" }] } },
  });
  document.getElementById("newFuncName").value = "";
  loadFunctions();
}

async function deleteFunction(name) {
  await api("DELETE", `${functionsBase()}/${name}?api-version=${API_FUNCTIONS}`);
  loadFunctions();
}

async function syncFunctionTriggers() {
  const app = document.getElementById("funcAppInput").value.trim();
  if (!app) return;
  await api(
    "POST",
    `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/sites/${app}/syncfunctiontriggers?api-version=${API_FUNCTIONS}`
  );
  alert("syncfunctiontriggers OK");
}

async function listFunctionKeys() {
  const app = document.getElementById("funcAppInput").value.trim();
  if (!app) return;
  const data = await api(
    "POST",
    `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/sites/${app}/host/default/listkeys?api-version=${API_FUNCTIONS}`
  );
  alert(JSON.stringify(data, null, 2));
}

// ---- initial load ----
refreshAll();
