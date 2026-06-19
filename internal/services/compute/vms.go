package compute

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const virtualMachinesBucket = "compute.virtualmachines"

const computeProvider = "Microsoft.Compute"

// VirtualMachine replica la forma estándar de ARM para
// Microsoft.Compute/virtualMachines.
type VirtualMachine struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Properties VirtualMachineProperties `json:"properties"`
}

type VirtualMachineProperties struct {
	ProvisioningState string          `json:"provisioningState"`
	HardwareProfile   HardwareProfile `json:"hardwareProfile"`
	StorageProfile    StorageProfile  `json:"storageProfile"`
	OsProfile         OsProfile       `json:"osProfile"`
	NetworkProfile    NetworkProfile  `json:"networkProfile"`
	InstanceView      InstanceView    `json:"instanceView"`
}

type HardwareProfile struct {
	VmSize string `json:"vmSize"`
}

type StorageProfile struct {
	ImageReference ImageReference `json:"imageReference"`
	OsDisk         OsDisk         `json:"osDisk"`
}

type OsDisk struct {
	Name         string      `json:"name"`
	CreateOption string      `json:"createOption"`
	ManagedDisk  ManagedDisk `json:"managedDisk"`
	DiskSizeGB   int         `json:"diskSizeGB,omitempty"`
}

type ManagedDisk struct {
	StorageAccountType string `json:"storageAccountType"`
}

// OsProfile replica "properties.osProfile" de una VM. AdminPassword se
// acepta en el request pero, como en Azure real, nunca se devuelve en la
// respuesta (de ahí que no tenga el tag json en este struct de salida).
type OsProfile struct {
	ComputerName  string `json:"computerName"`
	AdminUsername string `json:"adminUsername"`
}

type NetworkProfile struct {
	NetworkInterfaces []NetworkInterfaceReference `json:"networkInterfaces"`
}

// NetworkInterfaceReference replica el patrón de "referencia por ID" que ya
// usa Microsoft.Network (ver network.SubnetReference) — aquí apuntando a la
// NIC en vez de a la subnet.
type NetworkInterfaceReference struct {
	ID string `json:"id"`
}

type InstanceView struct {
	Statuses []VMStatus `json:"statuses"`
}

type VMStatus struct {
	Code          string `json:"code"`
	DisplayStatus string `json:"displayStatus"`
}

type virtualMachineRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		HardwareProfile HardwareProfile `json:"hardwareProfile"`
		StorageProfile  StorageProfile  `json:"storageProfile"`
		OsProfile       struct {
			ComputerName  string `json:"computerName"`
			AdminUsername string `json:"adminUsername"`
			AdminPassword string `json:"adminPassword"`
		} `json:"osProfile"`
		NetworkProfile NetworkProfile `json:"networkProfile"`
	} `json:"properties"`
}

func (s *Service) registerVirtualMachines(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines",
		s.listVirtualMachines)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines/{vmName}",
		s.putVirtualMachine)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines/{vmName}",
		s.getVirtualMachine)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines/{vmName}",
		s.deleteVirtualMachine)
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines/{vmName}/start",
		s.startVirtualMachine)
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines/{vmName}/powerOff",
		s.powerOffVirtualMachine)
}

func vmKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func vmID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s", subID, rg, name)
}

func runningStatus() InstanceView {
	return InstanceView{Statuses: []VMStatus{
		{Code: "ProvisioningState/succeeded", DisplayStatus: "Provisioning succeeded"},
		{Code: "PowerState/running", DisplayStatus: "VM running"},
	}}
}

func stoppedStatus() InstanceView {
	return InstanceView{Statuses: []VMStatus{
		{Code: "ProvisioningState/succeeded", DisplayStatus: "Provisioning succeeded"},
		{Code: "PowerState/stopped", DisplayStatus: "VM stopped"},
	}}
}

// putVirtualMachine sigue el patrón "create-async" de storageaccounts.go:
// se construye el recurso completo ya con ProvisioningState "Succeeded",
// se registra la operación manualmente, y se responde 202 con el cuerpo
// del recurso (no un 202 vacío como en delete).
func (s *Service) putVirtualMachine(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vmName")

	var req virtualMachineRequest
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
	if strings.TrimSpace(req.Properties.HardwareProfile.VmSize) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.hardwareProfile.vmSize' es obligatorio")
		return
	}
	if strings.TrimSpace(req.Properties.OsProfile.ComputerName) == "" || strings.TrimSpace(req.Properties.OsProfile.AdminUsername) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"los campos 'properties.osProfile.computerName' y 'properties.osProfile.adminUsername' son obligatorios")
		return
	}
	if len(req.Properties.NetworkProfile.NetworkInterfaces) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.networkProfile.networkInterfaces' es obligatorio y debe tener al menos un elemento")
		return
	}

	resolvedImage, err := resolveImageReference(req.Properties.StorageProfile.ImageReference)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidImageReference", err.Error())
		return
	}

	for i, nicRef := range req.Properties.NetworkProfile.NetworkInterfaces {
		if strings.TrimSpace(nicRef.ID) == "" {
			server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
				fmt.Sprintf("networkInterfaces[%d].id es obligatorio", i))
			return
		}
		_, found, err := s.net.FindNICByID(nicRef.ID)
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		if !found {
			server.WriteError(w, http.StatusBadRequest, "InvalidNetworkInterfaceReference",
				fmt.Sprintf("networkInterfaces[%d].id no apunta a una network interface existente: %s", i, nicRef.ID))
			return
		}
	}

	osDisk := req.Properties.StorageProfile.OsDisk
	if strings.TrimSpace(osDisk.Name) == "" {
		osDisk.Name = name + "_OsDisk"
	}
	if strings.TrimSpace(osDisk.CreateOption) == "" {
		osDisk.CreateOption = "FromImage"
	}
	if strings.TrimSpace(osDisk.ManagedDisk.StorageAccountType) == "" {
		osDisk.ManagedDisk.StorageAccountType = "Standard_LRS"
	}

	key := vmKey(subID, rg, name)
	_, found, err := s.getVM(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	vm := VirtualMachine{
		ID:       vmID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Compute/virtualMachines",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: VirtualMachineProperties{
			ProvisioningState: "Succeeded",
			HardwareProfile:   req.Properties.HardwareProfile,
			StorageProfile: StorageProfile{
				ImageReference: resolvedImage,
				OsDisk:         osDisk,
			},
			OsProfile: OsProfile{
				ComputerName:  req.Properties.OsProfile.ComputerName,
				AdminUsername: req.Properties.OsProfile.AdminUsername,
			},
			NetworkProfile: req.Properties.NetworkProfile,
			InstanceView:   runningStatus(),
		},
	}

	if err := s.db.Put(virtualMachinesBucket, key, vm); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Patrón create-async: la operación ya está "Succeeded" pero igual se
	// expone Azure-AsyncOperation/Location para que el polling de az
	// CLI/Terraform funcione, y se responde 202 con el cuerpo del recurso.
	id := s.ops.Succeeded()
	url := server.AsyncOperationURL(r, subID, computeProvider, req.Location, id, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	_ = found // el status code de creación real de Azure para VM PUT es 200/201 vía polling; aquí siempre 202 (ver comentario arriba)
	server.WriteJSON(w, http.StatusAccepted, vm)
}

func (s *Service) getVirtualMachine(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vmName")

	vm, found, err := s.getVM(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual machine '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, vm)
}

func (s *Service) listVirtualMachines(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	vms := make([]VirtualMachine, 0)
	err := s.db.List(virtualMachinesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var vm VirtualMachine
		if err := json.Unmarshal(raw, &vm); err != nil {
			return err
		}
		vms = append(vms, vm)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": vms})
}

// deleteVirtualMachine sigue el patrón "delete-async" de resourcemanager.go:
// se borra de forma síncrona y luego se responde con un 202 vacío vía
// server.WriteAccepted.
func (s *Service) deleteVirtualMachine(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vmName")
	key := vmKey(subID, rg, name)

	vm, found, err := s.getVM(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(virtualMachinesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteAccepted(w, r, s.ops, subID, computeProvider, vm.Location, apiVersion)
}

// startVirtualMachine y powerOffVirtualMachine emulan las acciones POST
// .../start y .../powerOff que az CLI/Terraform (vía azapi o az vm
// start/stop) usan para encender/apagar una VM ya creada. Ambas son
// async-only (sin cuerpo), igual que en Azure real.
func (s *Service) startVirtualMachine(w http.ResponseWriter, r *http.Request) {
	s.setPowerState(w, r, runningStatus())
}

func (s *Service) powerOffVirtualMachine(w http.ResponseWriter, r *http.Request) {
	s.setPowerState(w, r, stoppedStatus())
}

func (s *Service) setPowerState(w http.ResponseWriter, r *http.Request, view InstanceView) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("vmName")
	key := vmKey(subID, rg, name)

	vm, found, err := s.getVM(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la virtual machine '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	vm.Properties.InstanceView = view
	if err := s.db.Put(virtualMachinesBucket, key, vm); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteAccepted(w, r, s.ops, subID, computeProvider, vm.Location, apiVersion)
}

func (s *Service) getVM(subID, rg, name string) (VirtualMachine, bool, error) {
	var vm VirtualMachine
	found, err := s.db.Get(virtualMachinesBucket, vmKey(subID, rg, name), &vm)
	return vm, found, err
}
