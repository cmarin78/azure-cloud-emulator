package containerinstances

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const groupBucket = "containerinstances.groups"

// ---------- ARM response structs (match go-azure-sdk 2023-05-01) ----------

// ContainerGroup es la representación ARM de Microsoft.ContainerInstance/containerGroups.
// Properties.Containers, Properties.OsType, Properties.IPAddress son los
// campos que el provider azurerm_container_group lee en su Read.
type ContainerGroup struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Properties ContainerGroupProperties `json:"properties"`
}

// ContainerGroupProperties contiene el estado del grupo.
// Sku es "Standard" | "Dedicated" | "Confidential" (ForceNew en azurerm_container_group).
// Debe estar presente en el GET para que el provider no detecte drift.
type ContainerGroupProperties struct {
	Sku               string      `json:"sku"`
	Containers        []Container `json:"containers"`
	OsType            string      `json:"osType"`
	RestartPolicy     string      `json:"restartPolicy,omitempty"`
	ProvisioningState string      `json:"provisioningState"`
	IPAddress         *IPAddress  `json:"ipAddress,omitempty"`
}

// Container es un contenedor dentro del grupo.
type Container struct {
	Name       string              `json:"name"`
	Properties ContainerProperties `json:"properties"`
}

// ContainerProperties son las propiedades de un contenedor individual.
type ContainerProperties struct {
	Image     string               `json:"image"`
	Resources ResourceRequirements `json:"resources"`
	Ports     []ContainerPort      `json:"ports,omitempty"`
	Command   []string             `json:"command,omitempty"`
}

// ResourceRequirements especifica la CPU y memoria solicitadas.
type ResourceRequirements struct {
	Requests ResourceRequests `json:"requests"`
}

// ResourceRequests son las cantidades de recursos.
type ResourceRequests struct {
	CPU        float64 `json:"cpu"`
	MemoryInGB float64 `json:"memoryInGB"`
}

// ContainerPort es un puerto expuesto por un contenedor.
type ContainerPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

// IPAddress describe la dirección IP pública (o privada) del grupo.
type IPAddress struct {
	Type  string      `json:"type"`
	IP    string      `json:"ip,omitempty"`
	Fqdn  string      `json:"fqdn,omitempty"`
	Ports []GroupPort `json:"ports"`
}

// GroupPort es un puerto expuesto a nivel del grupo (diferente de ContainerPort).
type GroupPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

// ---------- request struct (acepta el body del provider) ----------

// containerGroupRequest es el body del PUT/PATCH que manda el provider.
type containerGroupRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		Sku           string                 `json:"sku,omitempty"`
		Containers    []containerRequestItem `json:"containers"`
		OsType        string                 `json:"osType"`
		RestartPolicy string                 `json:"restartPolicy,omitempty"`
		IPAddress     *ipAddressRequest      `json:"ipAddress,omitempty"`
	} `json:"properties"`
}

type containerRequestItem struct {
	Name       string `json:"name"`
	Properties struct {
		Image     string               `json:"image"`
		Resources ResourceRequirements `json:"resources"`
		Ports     []ContainerPort      `json:"ports,omitempty"`
		Command   []string             `json:"command,omitempty"`
	} `json:"properties"`
}

type ipAddressRequest struct {
	Type  string      `json:"type"`
	Ports []GroupPort `json:"ports"`
}

// ---------- helpers ----------

func groupKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

// fakeIP genera una IP determinista a partir del nombre del grupo.
func fakeIP(name string) string {
	h := sha256.Sum256([]byte(name))
	return fmt.Sprintf("10.0.%d.%d", h[0], h[1])
}

// fakeFQDN genera un FQDN determinista para grupos con IP pública.
func fakeFQDN(name, location string) string {
	h := sha256.Sum256([]byte(name))
	label := fmt.Sprintf("%x", h[:4])
	return label + "." + location + ".azurecontainer.io"
}

func osTypeOrDefault(s string) string {
	if s != "" {
		return s
	}
	return "Linux"
}

func restartPolicyOrDefault(s string) string {
	if s != "" {
		return s
	}
	return "Always"
}

func skuOrDefault(s string) string {
	if s != "" {
		return s
	}
	return "Standard"
}

// ---------- route registration ----------

func (s *Service) registerGroups(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerInstance/containerGroups"
	mux.HandleFunc("GET "+base, s.listGroups)
	mux.HandleFunc("PUT "+base+"/{groupName}", s.putGroup)
	mux.HandleFunc("PATCH "+base+"/{groupName}", s.putGroup)
	mux.HandleFunc("GET "+base+"/{groupName}", s.getGroupHandler)
	mux.HandleFunc("DELETE "+base+"/{groupName}", s.deleteGroup)
}

// ---------- handlers ----------

func (s *Service) putGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("groupName")

	var req containerGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	id := "/subscriptions/" + subID + "/resourceGroups/" + rg +
		"/providers/Microsoft.ContainerInstance/containerGroups/" + name

	// Construir contenedores
	containers := make([]Container, 0, len(req.Properties.Containers))
	for _, c := range req.Properties.Containers {
		containers = append(containers, Container{
			Name: c.Name,
			Properties: ContainerProperties{
				Image:     c.Properties.Image,
				Resources: c.Properties.Resources,
				Ports:     c.Properties.Ports,
				Command:   c.Properties.Command,
			},
		})
	}
	if len(containers) == 0 {
		// grupo sin contenedores (poco común pero aceptable en emulación)
		containers = []Container{}
	}

	// Construir dirección IP
	var ipAddr *IPAddress
	if req.Properties.IPAddress != nil {
		ip := &IPAddress{
			Type:  req.Properties.IPAddress.Type,
			Ports: req.Properties.IPAddress.Ports,
		}
		if ip.Type == "" {
			ip.Type = "Public"
		}
		if ip.Type == "Public" {
			ip.IP = fakeIP(name)
			ip.Fqdn = fakeFQDN(name, req.Location)
		}
		if len(ip.Ports) == 0 {
			ip.Ports = []GroupPort{}
		}
		ipAddr = ip
	}

	group := ContainerGroup{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.ContainerInstance/containerGroups",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: ContainerGroupProperties{
			Sku:               skuOrDefault(req.Properties.Sku),
			Containers:        containers,
			OsType:            osTypeOrDefault(req.Properties.OsType),
			RestartPolicy:     restartPolicyOrDefault(req.Properties.RestartPolicy),
			ProvisioningState: "Succeeded",
			IPAddress:         ipAddr,
		},
	}

	key := groupKey(subID, rg, name)
	_, existedBefore, err := s.getGroup(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Put(groupBucket, key, group); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, group)
}

func (s *Service) getGroup(subID, rg, name string) (ContainerGroup, bool, error) {
	var g ContainerGroup
	found, err := s.db.Get(groupBucket, groupKey(subID, rg, name), &g)
	return g, found, err
}

func (s *Service) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("groupName")

	g, found, err := s.getGroup(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el container group '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, g)
}

func (s *Service) listGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	groups := make([]ContainerGroup, 0)
	err := s.db.List(groupBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
		var g ContainerGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			return err
		}
		groups = append(groups, g)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": groups})
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("groupName")
	key := groupKey(subID, rg, name)

	found, err := s.db.Get(groupBucket, key, &ContainerGroup{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(groupBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
