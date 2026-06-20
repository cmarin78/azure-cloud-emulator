package aks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const agentPoolsBucket = "aks.agentpools"

// AgentPool replica Microsoft.ContainerService/managedClusters/agentPools.
// Igual que SecurityRule en network/nsg.go, vive anidado en
// properties.agentPoolProfiles del cluster padre pero también se expone
// como su propio sub-recurso ARM independientemente ruteable — es como
// azurerm_kubernetes_cluster_node_pool gestiona cualquier pool más allá del
// "default_node_pool" inline del cluster.
type AgentPool struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Properties AgentPoolProperties `json:"properties"`
}

type AgentPoolProperties struct {
	ProvisioningState string     `json:"provisioningState"`
	Count             int        `json:"count"`
	VMSize            string     `json:"vmSize"`
	OsDiskSizeGB      int        `json:"osDiskSizeGB,omitempty"`
	OsType            string     `json:"osType,omitempty"`
	Mode              string     `json:"mode"`
	PowerState        PowerState `json:"powerState"`
}

type agentPoolRequest struct {
	Properties struct {
		Count        int    `json:"count"`
		VMSize       string `json:"vmSize"`
		OsDiskSizeGB int    `json:"osDiskSizeGB,omitempty"`
		OsType       string `json:"osType,omitempty"`
		Mode         string `json:"mode,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerAgentPools(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName}"
	mux.HandleFunc("GET "+base+"/agentPools", s.listAgentPools)
	mux.HandleFunc("PUT "+base+"/agentPools/{agentPoolName}", s.putAgentPool)
	mux.HandleFunc("GET "+base+"/agentPools/{agentPoolName}", s.getAgentPool)
	mux.HandleFunc("DELETE "+base+"/agentPools/{agentPoolName}", s.deleteAgentPool)
}

func agentPoolKey(subID, rg, cluster, name string) string {
	return subID + "/" + rg + "/" + cluster + "/" + name
}

func agentPoolID(subID, rg, cluster, name string) string {
	return fmt.Sprintf("%s/agentPools/%s", clusterID(subID, rg, cluster), name)
}

// persistAgentPoolFromProfile guarda un AgentPoolProfile inline (recibido
// en el PUT del cluster padre) también como sub-recurso independientemente
// ruteable, para que GET .../agentPools/{name} lo vea de inmediato sin
// esperar un PUT explícito a esa ruta — igual que un az aks create con
// --node-count expone ese pool por defecto vía
// az aks nodepool show de entrada.
func (s *Service) persistAgentPoolFromProfile(subID, rg, cluster string, profile AgentPoolProfile) {
	ap := AgentPool{
		ID:   agentPoolID(subID, rg, cluster, profile.Name),
		Name: profile.Name,
		Type: "Microsoft.ContainerService/managedClusters/agentPools",
		Properties: AgentPoolProperties{
			ProvisioningState: "Succeeded",
			Count:             profile.Count,
			VMSize:            profile.VMSize,
			OsDiskSizeGB:      profile.OsDiskSizeGB,
			OsType:            profile.OsType,
			Mode:              profile.Mode,
			PowerState:        runningPowerState(),
		},
	}
	_ = s.db.Put(agentPoolsBucket, agentPoolKey(subID, rg, cluster, profile.Name), ap)
}

// putAgentPool sigue el mismo patrón async que putManagedCluster: requiere
// que el cluster padre ya exista (mismo no-validación de referencia que
// usa el resto del proyecto para "padre debe existir, hijo no valida
// referencias cruzadas" — aquí sí se exige el padre porque sin él no hay
// ni siquiera una location/provider válidos para el LRO).
func (s *Service) putAgentPool(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	clusterName := r.PathValue("clusterName")
	poolName := r.PathValue("agentPoolName")

	cluster, found, err := s.getCluster(subID, rg, clusterName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el managed cluster '%s' no existe en el resource group '%s'", clusterName, rg))
		return
	}

	var req agentPoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.VMSize) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.vmSize' es obligatorio")
		return
	}
	mode := req.Properties.Mode
	if mode == "" {
		mode = "User"
	} else if mode != "System" && mode != "User" {
		server.WriteError(w, http.StatusBadRequest, "InvalidAgentPoolMode",
			fmt.Sprintf("el campo 'properties.mode' debe ser 'System' o 'User', se recibió '%s'", mode))
		return
	}
	count := req.Properties.Count
	if count == 0 {
		count = 1
	}
	osType := req.Properties.OsType
	if osType == "" {
		osType = "Linux"
	}

	ap := AgentPool{
		ID:   agentPoolID(subID, rg, clusterName, poolName),
		Name: poolName,
		Type: "Microsoft.ContainerService/managedClusters/agentPools",
		Properties: AgentPoolProperties{
			ProvisioningState: "Succeeded",
			Count:             count,
			VMSize:            req.Properties.VMSize,
			OsDiskSizeGB:      req.Properties.OsDiskSizeGB,
			OsType:            osType,
			Mode:              mode,
			PowerState:        runningPowerState(),
		},
	}
	if err := s.db.Put(agentPoolsBucket, agentPoolKey(subID, rg, clusterName, poolName), ap); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	opID := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, containerServiceProvider, cluster.Location, opID, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	server.WriteJSON(w, http.StatusAccepted, ap)
}

func (s *Service) getAgentPool(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	clusterName := r.PathValue("clusterName")
	poolName := r.PathValue("agentPoolName")

	var ap AgentPool
	found, err := s.db.Get(agentPoolsBucket, agentPoolKey(subID, rg, clusterName, poolName), &ap)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el agent pool '%s' no existe en el cluster '%s'", poolName, clusterName))
		return
	}
	server.WriteJSON(w, http.StatusOK, ap)
}

func (s *Service) listAgentPools(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	clusterName := r.PathValue("clusterName")

	pools := make([]AgentPool, 0)
	err := s.db.List(agentPoolsBucket, subID+"/"+rg+"/"+clusterName+"/", func(key string, raw []byte) error {
		var ap AgentPool
		if err := json.Unmarshal(raw, &ap); err != nil {
			return err
		}
		pools = append(pools, ap)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": pools})
}

// deleteAgentPool sigue el patrón "delete-async" compartido por el resto
// del proyecto: 204 idempotente si ya no existe, 202 con
// Azure-AsyncOperation/Location si se borró.
func (s *Service) deleteAgentPool(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	clusterName := r.PathValue("clusterName")
	poolName := r.PathValue("agentPoolName")

	cluster, found, err := s.getCluster(subID, rg, clusterName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var existing AgentPool
	apFound, err := s.db.Get(agentPoolsBucket, agentPoolKey(subID, rg, clusterName, poolName), &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !apFound {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(agentPoolsBucket, agentPoolKey(subID, rg, clusterName, poolName)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteAccepted(w, r, s.ops, subID, containerServiceProvider, cluster.Location, apiVersion)
}

// deleteAgentPoolsForCluster borra todos los agent pools de un cluster,
// llamado desde deleteManagedCluster en cluster.go para que el borrado en
// cascada coincida con el comportamiento real de Azure (borrar un cluster
// borra todos sus node pools).
func (s *Service) deleteAgentPoolsForCluster(subID, rg, cluster string) error {
	prefix := subID + "/" + rg + "/" + cluster + "/"
	var keys []string
	err := s.db.List(agentPoolsBucket, prefix, func(key string, raw []byte) error {
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.db.Delete(agentPoolsBucket, key); err != nil {
			return err
		}
	}
	return nil
}
