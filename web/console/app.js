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
  loadAppConfig();
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

// ---- Phase 15: Event Hubs (Microsoft.EventHub, api-version 2021-11-01) ----
const API_EH = "2021-11-01";

function ehBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.EventHub/namespaces`;
}

async function loadEHNamespaces() {
  const data = await api("GET", `${ehBase()}?api-version=${API_EH}`);
  const rows = (data.value || [])
    .map(
      (ns) => `<tr>
        <td>${ns.name}</td>
        <td>${ns.location}</td>
        <td>${ns.sku?.name ?? ""}</td>
        <td>${ns.properties?.provisioningState ?? ""}</td>
        <td>${ns.properties?.serviceBusEndpoint ?? ""}</td>
        <td><button class="link" onclick="deleteEHNamespace('${ns.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("ehTable").innerHTML = rows;
}

async function createEHNamespace() {
  const name = document.getElementById("newEhName").value.trim();
  const location = document.getElementById("newEhLocation").value.trim();
  const skuName = document.getElementById("newEhSku").value;
  if (!name || !location) return;
  await api("PUT", `${ehBase()}/${name}?api-version=${API_EH}`, {
    location,
    sku: { name: skuName, tier: skuName },
  });
  document.getElementById("newEhName").value = "";
  loadEHNamespaces();
}

async function deleteEHNamespace(name) {
  await api("DELETE", `${ehBase()}/${name}?api-version=${API_EH}`);
  loadEHNamespaces();
}

// ---- Phase 16: Event Grid (Microsoft.EventGrid, api-version 2022-06-15) ----
const API_EG = "2022-06-15";

function egBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.EventGrid/topics`;
}

async function loadEGTopics() {
  const data = await api("GET", `${egBase()}?api-version=${API_EG}`);
  const rows = (data.value || [])
    .map(
      (t) => `<tr>
        <td>${t.name}</td>
        <td>${t.location}</td>
        <td>${t.properties?.provisioningState ?? ""}</td>
        <td>${t.properties?.endpoint ?? ""}</td>
        <td><button class="link" onclick="deleteEGTopic('${t.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("egTable").innerHTML = rows;
}

async function createEGTopic() {
  const name = document.getElementById("newEgName").value.trim();
  const location = document.getElementById("newEgLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${egBase()}/${name}?api-version=${API_EG}`, { location });
  document.getElementById("newEgName").value = "";
  loadEGTopics();
}

async function deleteEGTopic(name) {
  await api("DELETE", `${egBase()}/${name}?api-version=${API_EG}`);
  loadEGTopics();
}

// ---- Phase 17: Managed Identity (Microsoft.ManagedIdentity, api-version 2023-01-31) ----
const API_MI = "2023-01-31";

function miBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ManagedIdentity/userAssignedIdentities`;
}

async function loadManagedIdentities() {
  const data = await api("GET", `${miBase()}?api-version=${API_MI}`);
  const rows = (data.value || [])
    .map(
      (id) => `<tr>
        <td>${id.name}</td>
        <td>${id.location}</td>
        <td>${id.properties?.clientId ?? ""}</td>
        <td>${id.properties?.principalId ?? ""}</td>
        <td><button class="link" onclick="deleteManagedIdentity('${id.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("miTable").innerHTML = rows;
}

async function createManagedIdentity() {
  const name = document.getElementById("newMiName").value.trim();
  const location = document.getElementById("newMiLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${miBase()}/${name}?api-version=${API_MI}`, { location });
  document.getElementById("newMiName").value = "";
  loadManagedIdentities();
}

async function deleteManagedIdentity(name) {
  await api("DELETE", `${miBase()}/${name}?api-version=${API_MI}`);
  loadManagedIdentities();
}

// ---- Phase 18: API Management (Microsoft.ApiManagement, api-version 2022-08-01) ----
const API_APIM = "2022-08-01";

function apimBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ApiManagement/service`;
}

async function loadAPIM() {
  const data = await api("GET", `${apimBase()}?api-version=${API_APIM}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.sku?.name ?? ""}</td>
        <td>${s.properties?.provisioningState ?? ""}</td>
        <td>${s.properties?.gatewayUrl ?? ""}</td>
        <td><button class="link" onclick="deleteAPIM('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("apimTable").innerHTML = rows;
}

async function createAPIM() {
  const name = document.getElementById("newApimName").value.trim();
  const location = document.getElementById("newApimLocation").value.trim();
  const email = document.getElementById("newApimEmail").value.trim() || "admin@example.com";
  const publisher = document.getElementById("newApimPublisher").value.trim() || "Emulator";
  if (!name || !location) return;
  await api("PUT", `${apimBase()}/${name}?api-version=${API_APIM}`, {
    location,
    sku: { name: "Developer", capacity: 1 },
    properties: { publisherEmail: email, publisherName: publisher },
  });
  document.getElementById("newApimName").value = "";
  loadAPIM();
}

async function deleteAPIM(name) {
  await api("DELETE", `${apimBase()}/${name}?api-version=${API_APIM}`);
  loadAPIM();
}

// ---- Phase 21: Logic Apps (Microsoft.Logic, api-version 2019-05-01) ----
const API_LA = "2019-05-01";

function logicBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Logic/workflows`;
}

async function loadLogicApps() {
  const data = await api("GET", `${logicBase()}?api-version=${API_LA}`);
  const rows = (data.value || [])
    .map(
      (w) => `<tr>
        <td>${w.name}</td>
        <td>${w.location}</td>
        <td>${w.properties?.provisioningState ?? ""}</td>
        <td>${w.properties?.accessEndpoint ?? ""}</td>
        <td>
          <button class="link" onclick="triggerLogicApp('${w.name}')">Disparar</button>
          <button class="link" onclick="deleteLogicApp('${w.name}')">Borrar</button>
        </td>
      </tr>`
    )
    .join("");
  document.getElementById("laTable").innerHTML = rows;
}

async function createLogicApp() {
  const name = document.getElementById("newLaName").value.trim();
  const location = document.getElementById("newLaLocation").value.trim();
  if (!name || !location) return;
  await api("PUT", `${logicBase()}/${name}?api-version=${API_LA}`, {
    location,
    properties: {
      definition: {
        "$schema": "https://schema.management.azure.com/providers/Microsoft.Logic/schemas/2016-06-01/workflowdefinition.json#",
        contentVersion: "1.0.0.0",
        triggers: {},
        actions: {},
      },
      state: "Enabled",
    },
  });
  document.getElementById("newLaName").value = "";
  loadLogicApps();
}

async function triggerLogicApp(name) {
  await api("POST", `${logicBase()}/${name}/triggers/manual/run?api-version=${API_LA}`);
  alert(`Workflow '${name}' disparado`);
}

async function deleteLogicApp(name) {
  await api("DELETE", `${logicBase()}/${name}?api-version=${API_LA}`);
  loadLogicApps();
}

// ---- Phase 23: SQL Database (Microsoft.Sql, api-version 2022-05-01-preview) ----
const API_SQL = "2022-05-01-preview";

function sqlServerBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Sql/servers`;
}

async function loadSQLServers() {
  const data = await api("GET", `${sqlServerBase()}?api-version=${API_SQL}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.properties?.state ?? ""}</td>
        <td>${s.properties?.fullyQualifiedDomainName ?? ""}</td>
        <td><button class="link" onclick="deleteSQLServer('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("sqlSrvTable").innerHTML = rows;
}

async function createSQLServer() {
  const name = document.getElementById("newSqlSrvName").value.trim();
  const location = document.getElementById("newSqlSrvLocation").value.trim();
  const login = document.getElementById("newSqlSrvLogin").value.trim() || "sqladmin";
  if (!name || !location) return;
  await api("PUT", `${sqlServerBase()}/${name}?api-version=${API_SQL}`, {
    location,
    properties: {
      administratorLogin: login,
      administratorLoginPassword: "P@ssw0rd1234!",
      version: "12.0",
    },
  });
  document.getElementById("newSqlSrvName").value = "";
  loadSQLServers();
}

async function deleteSQLServer(name) {
  await api("DELETE", `${sqlServerBase()}/${name}?api-version=${API_SQL}`);
  loadSQLServers();
}

async function loadSQLDatabases() {
  const srv = document.getElementById("sqlSrvFilter").value.trim();
  if (!srv) { document.getElementById("sqlDbTable").innerHTML = ""; return; }
  const data = await api("GET", `${sqlServerBase()}/${srv}/databases?api-version=${API_SQL}`);
  const rows = (data.value || [])
    .map(
      (d) => `<tr>
        <td>${d.name}</td>
        <td>${d.sku?.name ?? ""}</td>
        <td>${d.properties?.status ?? ""}</td>
        <td><button class="link" onclick="deleteSQLDatabase('${srv}', '${d.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("sqlDbTable").innerHTML = rows;
}

async function createSQLDatabase() {
  const srv = document.getElementById("sqlSrvFilter").value.trim();
  const name = document.getElementById("newSqlDbName").value.trim();
  const sku = document.getElementById("newSqlDbSku").value;
  if (!srv || !name) return;
  const srvData = await api("GET", `${sqlServerBase()}/${srv}?api-version=${API_SQL}`);
  await api("PUT", `${sqlServerBase()}/${srv}/databases/${name}?api-version=${API_SQL}`, {
    location: srvData.location,
    sku: { name: sku },
  });
  document.getElementById("newSqlDbName").value = "";
  loadSQLDatabases();
}

async function deleteSQLDatabase(srv, name) {
  await api("DELETE", `${sqlServerBase()}/${srv}/databases/${name}?api-version=${API_SQL}`);
  loadSQLDatabases();
}

// ---- Phase 24: Container Registry (Microsoft.ContainerRegistry, api-version 2023-01-01-preview) ----
const API_ACR = "2023-01-01-preview";

function acrBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ContainerRegistry/registries`;
}

async function loadACRs() {
  const data = await api("GET", `${acrBase()}?api-version=${API_ACR}`);
  const rows = (data.value || [])
    .map(
      (r) => `<tr>
        <td>${r.name}</td>
        <td>${r.location}</td>
        <td>${r.sku?.name ?? ""}</td>
        <td>${r.properties?.provisioningState ?? ""}</td>
        <td>${r.properties?.loginServer ?? ""}</td>
        <td><button class="link" onclick="deleteACR('${r.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("acrTable").innerHTML = rows;
}

async function createACR() {
  const name = document.getElementById("newAcrName").value.trim();
  const location = document.getElementById("newAcrLocation").value.trim();
  const sku = document.getElementById("newAcrSku").value;
  if (!name || !location) return;
  await api("PUT", `${acrBase()}/${name}?api-version=${API_ACR}`, {
    location,
    sku: { name: sku },
    properties: { adminUserEnabled: true },
  });
  document.getElementById("newAcrName").value = "";
  loadACRs();
}

async function deleteACR(name) {
  await api("DELETE", `${acrBase()}/${name}?api-version=${API_ACR}`);
  loadACRs();
}

// ---- Phase 25: Redis Cache (Microsoft.Cache, api-version 2023-08-01) ----
const API_REDIS = "2023-08-01";

function redisBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Cache/redis`;
}

async function loadRedis() {
  const data = await api("GET", `${redisBase()}?api-version=${API_REDIS}`);
  const rows = (data.value || [])
    .map(
      (r) => `<tr>
        <td>${r.name}</td>
        <td>${r.location}</td>
        <td>${r.properties?.sku?.name ?? ""} C${r.properties?.sku?.capacity ?? ""}</td>
        <td>${r.properties?.provisioningState ?? ""}</td>
        <td>${r.properties?.hostName ?? ""}</td>
        <td><button class="link" onclick="deleteRedis('${r.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("redisTable").innerHTML = rows;
}

async function createRedis() {
  const name = document.getElementById("newRedisName").value.trim();
  const location = document.getElementById("newRedisLocation").value.trim();
  const skuName = document.getElementById("newRedisSku").value;
  const capacityMap = { Basic: 0, Standard: 1, Premium: 1 };
  const familyMap = { Basic: "C", Standard: "C", Premium: "P" };
  if (!name || !location) return;
  await api("PUT", `${redisBase()}/${name}?api-version=${API_REDIS}`, {
    location,
    properties: {
      sku: { name: skuName, family: familyMap[skuName] || "C", capacity: capacityMap[skuName] ?? 1 },
    },
  });
  document.getElementById("newRedisName").value = "";
  loadRedis();
}

async function deleteRedis(name) {
  await api("DELETE", `${redisBase()}/${name}?api-version=${API_REDIS}`);
  loadRedis();
}

// ---- Phase 26: Container Instances (Microsoft.ContainerInstance, api-version 2023-05-01) ----
const API_ACI = "2023-05-01";

function aciBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.ContainerInstance/containerGroups`;
}

async function loadACIs() {
  const data = await api("GET", `${aciBase()}?api-version=${API_ACI}`);
  const rows = (data.value || [])
    .map(
      (g) => `<tr>
        <td>${g.name}</td>
        <td>${g.location}</td>
        <td>${g.properties?.provisioningState ?? ""}</td>
        <td>${g.properties?.ipAddress?.ip ?? ""}</td>
        <td>${g.properties?.ipAddress?.fqdn ?? ""}</td>
        <td><button class="link" onclick="deleteACI('${g.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("aciTable").innerHTML = rows;
}

async function createACI() {
  const name = document.getElementById("newAciName").value.trim();
  const location = document.getElementById("newAciLocation").value.trim();
  const image = document.getElementById("newAciImage").value.trim() || "nginx:latest";
  if (!name || !location) return;
  await api("PUT", `${aciBase()}/${name}?api-version=${API_ACI}`, {
    location,
    properties: {
      osType: "Linux",
      ipAddress: { type: "Public", ports: [{ port: 80, protocol: "TCP" }] },
      containers: [{
        name: "container1",
        properties: { image, resources: { requests: { cpu: 1, memoryInGB: 1.5 } }, ports: [{ port: 80 }] },
      }],
    },
  });
  document.getElementById("newAciName").value = "";
  loadACIs();
}

async function deleteACI(name) {
  await api("DELETE", `${aciBase()}/${name}?api-version=${API_ACI}`);
  loadACIs();
}

// ---- Phase 27: Azure Policy (Microsoft.Authorization, api-version 2021-06-01) ----
const API_POLICY = "2021-06-01";

function policyDefBase() {
  return `/subscriptions/${sub()}/providers/Microsoft.Authorization/policyDefinitions`;
}

function policyAssignBase() {
  return `/subscriptions/${sub()}/providers/Microsoft.Authorization/policyAssignments`;
}

async function loadPolicyDefinitions() {
  const data = await api("GET", `${policyDefBase()}?api-version=${API_POLICY}`);
  const rows = (data.value || [])
    .map(
      (d) => `<tr>
        <td>${d.name}</td>
        <td>${d.properties?.displayName ?? ""}</td>
        <td>${d.properties?.policyType ?? ""}</td>
        <td>${d.properties?.mode ?? ""}</td>
        <td><button class="link" onclick="deletePolicyDefinition('${d.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("pdTable").innerHTML = rows;
}

async function createPolicyDefinition() {
  const name = document.getElementById("newPdName").value.trim();
  const displayName = document.getElementById("newPdDisplayName").value.trim() || name;
  if (!name) return;
  await api("PUT", `${policyDefBase()}/${name}?api-version=${API_POLICY}`, {
    properties: {
      policyType: "Custom",
      mode: "All",
      displayName,
      policyRule: { if: { field: "type", equals: "Microsoft.Network/publicIPAddresses" }, then: { effect: "audit" } },
    },
  });
  document.getElementById("newPdName").value = "";
  document.getElementById("newPdDisplayName").value = "";
  loadPolicyDefinitions();
}

async function deletePolicyDefinition(name) {
  await api("DELETE", `${policyDefBase()}/${name}?api-version=${API_POLICY}`);
  loadPolicyDefinitions();
}

async function loadPolicyAssignments() {
  const data = await api("GET", `${policyAssignBase()}?api-version=${API_POLICY}`);
  const rows = (data.value || [])
    .map(
      (a) => `<tr>
        <td>${a.name}</td>
        <td>${a.properties?.displayName ?? ""}</td>
        <td>${a.properties?.provisioningState ?? ""}</td>
        <td><button class="link" onclick="deletePolicyAssignment('${a.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("paTable").innerHTML = rows;
}

async function createPolicyAssignment() {
  const name = document.getElementById("newPaName").value.trim();
  const policyId = document.getElementById("newPaPolicyId").value.trim();
  if (!name || !policyId) return;
  await api("PUT", `${policyAssignBase()}/${name}?api-version=${API_POLICY}`, {
    properties: {
      policyDefinitionId: policyId,
      displayName: name,
      enforcementMode: "DoNotEnforce",
    },
  });
  document.getElementById("newPaName").value = "";
  document.getElementById("newPaPolicyId").value = "";
  loadPolicyAssignments();
}

async function deletePolicyAssignment(name) {
  await api("DELETE", `${policyAssignBase()}/${name}?api-version=${API_POLICY}`);
  loadPolicyAssignments();
}

// ---- Phase 29: Static Web Apps (Microsoft.Web/staticSites, api-version 2022-09-01) ----
const API_SWA = "2022-09-01";

function swaBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Web/staticSites`;
}

async function loadStaticSites() {
  const data = await api("GET", `${swaBase()}?api-version=${API_SWA}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.sku?.name ?? ""}</td>
        <td>${s.properties?.defaultHostname ?? ""}</td>
        <td><button class="link" onclick="deleteStaticSite('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("swaTable").innerHTML = rows;
}

async function createStaticSite() {
  const name = document.getElementById("newSwaName").value.trim();
  const location = document.getElementById("newSwaLocation").value.trim() || "eastus2";
  if (!name) return;
  await api("PUT", `${swaBase()}/${name}?api-version=${API_SWA}`, {
    location,
    sku: { name: "Free", tier: "Free" },
    properties: {},
  });
  document.getElementById("newSwaName").value = "";
  loadStaticSites();
}

async function deleteStaticSite(name) {
  await api("DELETE", `${swaBase()}/${name}?api-version=${API_SWA}`);
  loadStaticSites();
}

// ---- Phase 30: PostgreSQL Flexible Server (Microsoft.DBforPostgreSQL, api-version 2022-12-01) ----
const API_PG = "2022-12-01";

function pgBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.DBforPostgreSQL/flexibleServers`;
}

async function loadPostgres() {
  const data = await api("GET", `${pgBase()}?api-version=${API_PG}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.properties?.version ?? ""}</td>
        <td>${s.sku?.name ?? ""}</td>
        <td>${s.properties?.state ?? ""}</td>
        <td>${s.properties?.fullyQualifiedDomainName ?? ""}</td>
        <td><button class="link" onclick="deletePostgres('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("pgTable").innerHTML = rows;
}

async function createPostgres() {
  const name = document.getElementById("newPgName").value.trim();
  const location = document.getElementById("newPgLocation").value.trim();
  const version = document.getElementById("newPgVersion").value.trim() || "16";
  const login = document.getElementById("newPgLogin").value.trim() || "pgadmin";
  if (!name || !location) return;
  await api("PUT", `${pgBase()}/${name}?api-version=${API_PG}`, {
    location,
    sku: { name: "B_Standard_B1ms", tier: "Burstable" },
    properties: {
      administratorLogin: login,
      administratorLoginPassword: "P@ssw0rd1234!",
      version,
      storage: { storageSizeGB: 32 },
    },
  });
  document.getElementById("newPgName").value = "";
  loadPostgres();
}

async function deletePostgres(name) {
  await api("DELETE", `${pgBase()}/${name}?api-version=${API_PG}`);
  loadPostgres();
}

// ---- Phase 30: MySQL Flexible Server (Microsoft.DBforMySQL, api-version 2023-06-30) ----
const API_MY = "2023-06-30";

function myBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.DBforMySQL/flexibleServers`;
}

async function loadMySQL() {
  const data = await api("GET", `${myBase()}?api-version=${API_MY}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.properties?.version ?? ""}</td>
        <td>${s.sku?.name ?? ""}</td>
        <td>${s.properties?.state ?? ""}</td>
        <td>${s.properties?.fullyQualifiedDomainName ?? ""}</td>
        <td><button class="link" onclick="deleteMySQL('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("myTable").innerHTML = rows;
}

async function createMySQL() {
  const name = document.getElementById("newMyName").value.trim();
  const location = document.getElementById("newMyLocation").value.trim();
  const version = document.getElementById("newMyVersion").value.trim() || "8.0.21";
  const login = document.getElementById("newMyLogin").value.trim() || "mysqladmin";
  if (!name || !location) return;
  await api("PUT", `${myBase()}/${name}?api-version=${API_MY}`, {
    location,
    sku: { name: "Standard_B1ms", tier: "Burstable" },
    properties: {
      administratorLogin: login,
      administratorLoginPassword: "P@ssw0rd1234!",
      version,
      storage: { storageSizeGB: 20, autoGrow: "Enabled" },
    },
  });
  document.getElementById("newMyName").value = "";
  loadMySQL();
}

async function deleteMySQL(name) {
  await api("DELETE", `${myBase()}/${name}?api-version=${API_MY}`);
  loadMySQL();
}

// ---- Phase 31: Recovery Services Vault (Microsoft.RecoveryServices, api-version 2023-04-01) ----
const API_RSV = "2023-04-01";

function rsvBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.RecoveryServices/vaults`;
}

async function loadRSVs() {
  const data = await api("GET", `${rsvBase()}?api-version=${API_RSV}`);
  const rows = (data.value || [])
    .map(
      (v) => `<tr>
        <td>${v.name}</td>
        <td>${v.location}</td>
        <td>${v.sku?.name ?? ""}</td>
        <td>${v.properties?.provisioningState ?? ""}</td>
        <td><button class="link" onclick="deleteRSV('${v.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("rsvTable").innerHTML = rows;
}

async function createRSV() {
  const name = document.getElementById("newRsvName").value.trim();
  const location = document.getElementById("newRsvLocation").value.trim();
  const sku = document.getElementById("newRsvSku").value;
  if (!name || !location) return;
  await api("PUT", `${rsvBase()}/${name}?api-version=${API_RSV}`, {
    location,
    sku: { name: sku },
    properties: {},
  });
  document.getElementById("newRsvName").value = "";
  loadRSVs();
}

async function deleteRSV(name) {
  await api("DELETE", `${rsvBase()}/${name}?api-version=${API_RSV}`);
  loadRSVs();
}

// ---- Phase 32: Notification Hubs (Microsoft.NotificationHubs, api-version 2017-04-01) ----
const API_NH = "2017-04-01";

function nhNsBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.NotificationHubs/namespaces`;
}

async function loadNHNamespaces() {
  const data = await api("GET", `${nhNsBase()}?api-version=${API_NH}`);
  const rows = (data.value || [])
    .map(
      (ns) => `<tr>
        <td>${ns.name}</td>
        <td>${ns.location}</td>
        <td>${ns.sku?.name ?? ""}</td>
        <td>${ns.properties?.provisioningState ?? ""}</td>
        <td>${ns.properties?.serviceBusEndpoint ?? ""}</td>
        <td><button class="link" onclick="deleteNHNamespace('${ns.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("nhNsTable").innerHTML = rows;
}

async function createNHNamespace() {
  const name = document.getElementById("newNhNsName").value.trim();
  const location = document.getElementById("newNhNsLocation").value.trim();
  const sku = document.getElementById("newNhNsSku").value;
  if (!name || !location) return;
  await api("PUT", `${nhNsBase()}/${name}?api-version=${API_NH}`, {
    location,
    sku: { name: sku },
    properties: { namespaceType: "NotificationHub" },
  });
  document.getElementById("newNhNsName").value = "";
  loadNHNamespaces();
}

async function deleteNHNamespace(name) {
  await api("DELETE", `${nhNsBase()}/${name}?api-version=${API_NH}`);
  loadNHNamespaces();
}

async function loadNHHubs() {
  const ns = document.getElementById("nhNsFilter").value.trim();
  if (!ns) { document.getElementById("nhHubTable").innerHTML = ""; return; }
  const data = await api("GET", `${nhNsBase()}/${ns}/notificationHubs?api-version=${API_NH}`);
  const rows = (data.value || [])
    .map(
      (h) => `<tr>
        <td>${h.name}</td>
        <td><button class="link" onclick="deleteNHHub('${ns}', '${h.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("nhHubTable").innerHTML = rows;
}

async function createNHHub() {
  const ns = document.getElementById("nhNsFilter").value.trim();
  const name = document.getElementById("newNhHubName").value.trim();
  if (!ns || !name) return;
  const nsData = await api("GET", `${nhNsBase()}/${ns}?api-version=${API_NH}`);
  await api("PUT", `${nhNsBase()}/${ns}/notificationHubs/${name}?api-version=${API_NH}`, {
    location: nsData.location,
    properties: {},
  });
  document.getElementById("newNhHubName").value = "";
  loadNHHubs();
}

async function deleteNHHub(ns, name) {
  await api("DELETE", `${nhNsBase()}/${ns}/notificationHubs/${name}?api-version=${API_NH}`);
  loadNHHubs();
}

// ---- Phase 32: SignalR (Microsoft.SignalRService, api-version 2023-02-01) ----
const API_SR = "2023-02-01";

function srBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.SignalRService/signalR`;
}

async function loadSignalR() {
  const data = await api("GET", `${srBase()}?api-version=${API_SR}`);
  const rows = (data.value || [])
    .map(
      (s) => `<tr>
        <td>${s.name}</td>
        <td>${s.location}</td>
        <td>${s.sku?.name ?? ""}</td>
        <td>${s.properties?.provisioningState ?? ""}</td>
        <td>${s.properties?.hostName ?? ""}</td>
        <td><button class="link" onclick="deleteSignalR('${s.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("srTable").innerHTML = rows;
}

async function createSignalR() {
  const name = document.getElementById("newSrName").value.trim();
  const location = document.getElementById("newSrLocation").value.trim();
  const sku = document.getElementById("newSrSku").value;
  if (!name || !location) return;
  await api("PUT", `${srBase()}/${name}?api-version=${API_SR}`, {
    location,
    sku: { name: sku, capacity: 1 },
    properties: {},
  });
  document.getElementById("newSrName").value = "";
  loadSignalR();
}

async function deleteSignalR(name) {
  await api("DELETE", `${srBase()}/${name}?api-version=${API_SR}`);
  loadSignalR();
}

// ---- Phase 33: Private Endpoints (Microsoft.Network, api-version 2023-09-01) ----
const API_PE = "2023-09-01";

function peBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.Network/privateEndpoints`;
}

async function loadPrivateEndpoints() {
  const data = await api("GET", `${peBase()}?api-version=${API_PE}`);
  const rows = (data.value || [])
    .map(
      (pe) => `<tr>
        <td>${pe.name}</td>
        <td>${pe.location}</td>
        <td>${pe.properties?.provisioningState ?? ""}</td>
        <td>${pe.properties?.privateLinkServiceConnections?.[0]?.properties?.privateLinkServiceConnectionState?.status ?? ""}</td>
        <td><button class="link" onclick="deletePrivateEndpoint('${pe.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("peTable").innerHTML = rows;
}

async function createPrivateEndpoint() {
  const name = document.getElementById("newPeName").value.trim();
  const location = document.getElementById("newPeLocation").value.trim();
  const subnetId = document.getElementById("newPeSubnetId").value.trim();
  const resourceId = document.getElementById("newPeResourceId").value.trim();
  const groupId = document.getElementById("newPeGroupId").value.trim() || "blob";
  if (!name || !location || !subnetId || !resourceId) return;
  await api("PUT", `${peBase()}/${name}?api-version=${API_PE}`, {
    location,
    properties: {
      subnet: { id: subnetId },
      privateLinkServiceConnections: [{
        name: "conn1",
        properties: { privateLinkServiceId: resourceId, groupIds: [groupId] },
      }],
    },
  });
  document.getElementById("newPeName").value = "";
  document.getElementById("newPeSubnetId").value = "";
  document.getElementById("newPeResourceId").value = "";
  loadPrivateEndpoints();
}

async function deletePrivateEndpoint(name) {
  await api("DELETE", `${peBase()}/${name}?api-version=${API_PE}`);
  loadPrivateEndpoints();
}

// ---- Phase 34: App Configuration (Microsoft.AppConfiguration, api-version 2023-03-01) ----
const API_APPCONFIG = "2023-03-01";

function appConfigBase() {
  return `/subscriptions/${sub()}/resourceGroups/${rg()}/providers/Microsoft.AppConfiguration/configurationStores`;
}

async function loadAppConfig() {
  const data = await api("GET", `${appConfigBase()}?api-version=${API_APPCONFIG}`);
  const rows = (data.value || [])
    .map(
      (c) => `<tr>
        <td>${c.name}</td>
        <td>${c.location}</td>
        <td>${c.sku?.name ?? ""}</td>
        <td>${c.properties?.provisioningState ?? ""}</td>
        <td>${c.properties?.endpoint ?? ""}</td>
        <td><button class="link" onclick="deleteAppConfig('${c.name}')">Borrar</button></td>
      </tr>`
    )
    .join("");
  document.getElementById("appConfigTable").innerHTML = rows;
}

async function createAppConfig() {
  const name = document.getElementById("newAppConfigName").value.trim();
  const location = document.getElementById("newAppConfigLocation").value.trim();
  const sku = document.getElementById("newAppConfigSku").value;
  if (!name || !location) return;
  await api("PUT", `${appConfigBase()}/${name}?api-version=${API_APPCONFIG}`, {
    location,
    sku: { name: sku },
    properties: { publicNetworkAccess: "Enabled" },
  });
  document.getElementById("newAppConfigName").value = "";
  loadAppConfig();
}

async function deleteAppConfig(name) {
  await api("DELETE", `${appConfigBase()}/${name}?api-version=${API_APPCONFIG}`);
  loadAppConfig();
}

// ---- initial load ----
refreshAll();
