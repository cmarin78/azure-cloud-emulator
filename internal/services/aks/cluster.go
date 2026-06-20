package aks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const managedClustersBucket = "aks.managedclusters"

const containerServiceProvider = "Microsoft.ContainerService"

// ManagedCluster replica la forma estándar de ARM para
// Microsoft.ContainerService/managedClusters.
type ManagedCluster struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Identity   *Identity                `json:"identity,omitempty"`
	SKU        *ManagedClusterSKU       `json:"sku,omitempty"`
	Properties ManagedClusterProperties `json:"properties"`
}

// Identity replica el bloque "identity" estándar de ARM (igual shape que
// usan App Service/Key Vault cuando se les asigna una managed identity).
type Identity struct {
	Type        string `json:"type"`
	PrincipalID string `json:"principalId,omitempty"`
	TenantID    string `json:"tenantId,omitempty"`
}

// ManagedClusterSKU replica "sku" (azurerm_kubernetes_cluster's sku_tier
// maps here as "Free"/"Standard"/"Premium" vía properties, pero el campo
// sku.name/tier también existe en versiones recientes de la API).
type ManagedClusterSKU struct {
	Name string `json:"name,omitempty"`
	Tier string `json:"tier,omitempty"`
}

type ManagedClusterProperties struct {
	ProvisioningState string             `json:"provisioningState"`
	KubernetesVersion string             `json:"kubernetesVersion"`
	DNSPrefix         string             `json:"dnsPrefix"`
	Fqdn              string             `json:"fqdn,omitempty"`
	NodeResourceGroup string             `json:"nodeResourceGroup,omitempty"`
	EnableRBAC        bool               `json:"enableRBAC"`
	AgentPoolProfiles []AgentPoolProfile `json:"agentPoolProfiles"`
	NetworkProfile    *NetworkProfile    `json:"networkProfile,omitempty"`
	PowerState        PowerState         `json:"powerState"`
}

// AgentPoolProfile replica el shape inline de properties.agentPoolProfiles.
// Es el mismo conjunto de campos que AgentPool (el sub-recurso
// independientemente ruteable en agentpools.go) expone bajo su propio
// "properties" — se mantienen en sync vía agentPoolProfilesForCluster,
// igual que gcp-emulator/internal/services/gke mantiene Cluster.NodePools
// sincronizado con el bucket de node pools.
type AgentPoolProfile struct {
	Name              string     `json:"name"`
	Count             int        `json:"count"`
	VMSize            string     `json:"vmSize"`
	OsDiskSizeGB      int        `json:"osDiskSizeGB,omitempty"`
	OsType            string     `json:"osType,omitempty"`
	Mode              string     `json:"mode"`
	ProvisioningState string     `json:"provisioningState,omitempty"`
	PowerState        PowerState `json:"powerState,omitempty"`
}

// NetworkProfile replica el subconjunto más usado de
// properties.networkProfile.
type NetworkProfile struct {
	NetworkPlugin string `json:"networkPlugin,omitempty"`
	ServiceCidr   string `json:"serviceCidr,omitempty"`
	DNSServiceIP  string `json:"dnsServiceIP,omitempty"`
	PodCidr       string `json:"podCidr,omitempty"`
}

// PowerState replica properties.powerState (también usado dentro de cada
// agentPoolProfile).
type PowerState struct {
	Code string `json:"code"`
}

type managedClusterRequest struct {
	Location   string             `json:"location"`
	Tags       map[string]string  `json:"tags,omitempty"`
	Identity   *Identity          `json:"identity,omitempty"`
	SKU        *ManagedClusterSKU `json:"sku,omitempty"`
	Properties struct {
		KubernetesVersion string             `json:"kubernetesVersion"`
		DNSPrefix         string             `json:"dnsPrefix"`
		EnableRBAC        *bool              `json:"enableRBAC,omitempty"`
		AgentPoolProfiles []AgentPoolProfile `json:"agentPoolProfiles,omitempty"`
		NetworkProfile    *NetworkProfile    `json:"networkProfile,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerManagedClusters(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters",
		s.listManagedClusters)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}",
		s.putManagedCluster)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}",
		s.getManagedCluster)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}",
		s.deleteManagedCluster)
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}/listClusterUserCredential",
		s.listClusterUserCredential)
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}/listClusterAdminCredential",
		s.listClusterUserCredential)
}

func clusterKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func clusterID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s", subID, rg, name)
}

// fakeHexSuffix deriva un sufijo hexadecimal determinista a partir de un
// seed, mismo espíritu que fakePublicIP en network/publicip.go pero
// formateado como hex (lo que usa Azure real para el sufijo del fqdn y
// para los GUIDs simulados de identity).
func fakeHexSuffix(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

func fakeGUID(seed string) string {
	sum := fakeHexSuffix(seed)
	return fmt.Sprintf("%08x-0000-0000-0000-%012x", sum, uint64(sum)*2654435761)
}

func runningPowerState() PowerState { return PowerState{Code: "Running"} }

// defaultAgentPoolProfiles sintetiza un único pool de modo "System" cuando
// el request no trae properties.agentPoolProfiles, igual que GKE sintetiza
// un "default-pool" cuando el cliente no crea uno explícitamente — Azure
// real exige al menos un pool en modo System en todo cluster.
func defaultAgentPoolProfiles() []AgentPoolProfile {
	return []AgentPoolProfile{{
		Name:   "default",
		Count:  1,
		VMSize: "Standard_DS2_v2",
		OsType: "Linux",
		Mode:   "System",
	}}
}

// agentPoolProfilesForCluster construye properties.agentPoolProfiles desde
// el bucket de agentPools (agentpools.go), igual que GKE's
// nodePoolsForCluster — se lee fresco del storage en cada GET en vez de
// confiar en una copia cacheada, para que los pools creados después de la
// creación del cluster (vía PUT .../agentPools/{name}) aparezcan también en
// properties.agentPoolProfiles del cluster padre.
func (s *Service) agentPoolProfilesForCluster(subID, rg, cluster string) []AgentPoolProfile {
	prefix := subID + "/" + rg + "/" + cluster + "/"
	profiles := []AgentPoolProfile{}
	_ = s.db.List(agentPoolsBucket, prefix, func(key string, raw []byte) error {
		var ap AgentPool
		if err := json.Unmarshal(raw, &ap); err != nil {
			return err
		}
		profiles = append(profiles, AgentPoolProfile{
			Name:              ap.Name,
			Count:             ap.Properties.Count,
			VMSize:            ap.Properties.VMSize,
			OsDiskSizeGB:      ap.Properties.OsDiskSizeGB,
			OsType:            ap.Properties.OsType,
			Mode:              ap.Properties.Mode,
			ProvisioningState: ap.Properties.ProvisioningState,
			PowerState:        ap.Properties.PowerState,
		})
		return nil
	})
	return profiles
}

// putManagedCluster sigue el patrón "create-async" de compute/vms.go: el
// recurso se construye completo con provisioningState "Succeeded" y se
// responde 202 con Azure-AsyncOperation/Location además del cuerpo.
func (s *Service) putManagedCluster(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("clusterName")

	var req managedClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.DNSPrefix) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.dnsPrefix' es obligatorio")
		return
	}

	id := clusterID(subID, rg, name)

	agentPools := req.Properties.AgentPoolProfiles
	if len(agentPools) == 0 {
		agentPools = defaultAgentPoolProfiles()
	}
	for i := range agentPools {
		if strings.TrimSpace(agentPools[i].VMSize) == "" {
			server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
				fmt.Sprintf("agentPoolProfiles[%d].vmSize es obligatorio", i))
			return
		}
		if agentPools[i].Mode != "System" && agentPools[i].Mode != "User" {
			if agentPools[i].Mode == "" {
				agentPools[i].Mode = "System"
			} else {
				server.WriteError(w, http.StatusBadRequest, "InvalidAgentPoolMode",
					fmt.Sprintf("agentPoolProfiles[%d].mode debe ser 'System' o 'User', se recibió '%s'", i, agentPools[i].Mode))
				return
			}
		}
		if agentPools[i].Count == 0 {
			agentPools[i].Count = 1
		}
		if agentPools[i].OsType == "" {
			agentPools[i].OsType = "Linux"
		}
		agentPools[i].ProvisioningState = "Succeeded"
		agentPools[i].PowerState = runningPowerState()
	}

	kubernetesVersion := req.Properties.KubernetesVersion
	if kubernetesVersion == "" {
		kubernetesVersion = "1.29.2"
	}
	enableRBAC := true
	if req.Properties.EnableRBAC != nil {
		enableRBAC = *req.Properties.EnableRBAC
	}
	nodeResourceGroup := fmt.Sprintf("MC_%s_%s_%s", rg, name, req.Location)
	fqdn := fmt.Sprintf("%s-%08x.hcp.%s.azmk8s.io", req.Properties.DNSPrefix, fakeHexSuffix(id), req.Location)

	var identity *Identity
	if req.Identity != nil {
		identity = &Identity{Type: req.Identity.Type}
		if identity.Type != "" && identity.Type != "None" {
			identity.PrincipalID = fakeGUID(id + "-principal")
			identity.TenantID = fakeGUID(id + "-tenant")
		}
	}

	cluster := ManagedCluster{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.ContainerService/managedClusters",
		Location: req.Location,
		Tags:     req.Tags,
		Identity: identity,
		SKU:      req.SKU,
		Properties: ManagedClusterProperties{
			ProvisioningState: "Succeeded",
			KubernetesVersion: kubernetesVersion,
			DNSPrefix:         req.Properties.DNSPrefix,
			Fqdn:              fqdn,
			NodeResourceGroup: nodeResourceGroup,
			EnableRBAC:        enableRBAC,
			AgentPoolProfiles: agentPools,
			NetworkProfile:    req.Properties.NetworkProfile,
			PowerState:        runningPowerState(),
		},
	}

	// Persistir cada agentPoolProfile inline también como sub-recurso
	// independientemente ruteable (agentpools.go), igual que Azure real
	// expone properties.agentPoolProfiles[*] como
	// managedClusters/{name}/agentPools/{poolName}. Mantiene el mismo
	// patrón "sync inline + sub-recurso ruteable" que NSG security rules.
	for _, ap := range agentPools {
		s.persistAgentPoolFromProfile(subID, rg, name, ap)
	}

	if err := s.db.Put(managedClustersBucket, clusterKey(subID, rg, name), cluster); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	cluster.Properties.AgentPoolProfiles = s.agentPoolProfilesForCluster(subID, rg, name)
	opID := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, containerServiceProvider, req.Location, opID, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, cluster)
}

func (s *Service) getManagedCluster(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("clusterName")

	cluster, found, err := s.getCluster(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el managed cluster '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	cluster.Properties.AgentPoolProfiles = s.agentPoolProfilesForCluster(subID, rg, name)
	server.WriteJSON(w, http.StatusOK, cluster)
}

func (s *Service) listManagedClusters(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	clusters := make([]ManagedCluster, 0)
	err := s.db.List(managedClustersBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var c ManagedCluster
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		c.Properties.AgentPoolProfiles = s.agentPoolProfilesForCluster(subID, rg, c.Name)
		clusters = append(clusters, c)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": clusters})
}

// deleteManagedCluster sigue el patrón "delete-async" de compute/vms.go:
// borra de forma síncrona (incluyendo todos sus agentPools) y responde con
// un 202 vacío.
func (s *Service) deleteManagedCluster(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("clusterName")

	cluster, found, err := s.getCluster(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.deleteAgentPoolsForCluster(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Delete(managedClustersBucket, clusterKey(subID, rg, name)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteAccepted(w, r, s.ops, subID, containerServiceProvider, cluster.Location, apiVersion)
}

// listClusterUserCredential emula
// POST .../managedClusters/{name}/listClusterUserCredential (y, montado en
// la misma función, listClusterAdminCredential) — la acción que
// azurerm_kubernetes_cluster/az aks get-credentials usan para poblar
// kube_config/kube_admin_config. Es síncrona (sin LRO), igual que
// Monitor's sharedKeys. El kubeconfig devuelto es un YAML mínimo pero
// sintácticamente válido, con un server/cluster-ca/token fake — suficiente
// para que el provider decodifique el base64 y popule sus atributos sin
// intentar conectarse de verdad a un cluster.
func (s *Service) listClusterUserCredential(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("clusterName")

	cluster, found, err := s.getCluster(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el managed cluster '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: https://%s:443
    certificate-authority-data: %s
contexts:
- name: %s
  context:
    cluster: %s
    user: clusterUser_%s_%s
current-context: %s
users:
- name: clusterUser_%s_%s
  user:
    token: %s
`,
		name, cluster.Properties.Fqdn, base64.StdEncoding.EncodeToString([]byte("fake-ca-cert-"+name)),
		name, name, rg, name,
		name,
		rg, name, fakeGUID(cluster.ID+"-token"))

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"kubeconfigs": []map[string]any{
			{"name": "clusterUser", "value": base64.StdEncoding.EncodeToString([]byte(kubeconfig))},
		},
	})
}

func (s *Service) getCluster(subID, rg, name string) (ManagedCluster, bool, error) {
	var cluster ManagedCluster
	found, err := s.db.Get(managedClustersBucket, clusterKey(subID, rg, name), &cluster)
	return cluster, found, err
}
