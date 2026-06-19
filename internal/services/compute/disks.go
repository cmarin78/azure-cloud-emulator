package compute

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const disksBucket = "compute.disks"

// DiskSku replica "sku" de un managed disk (p. ej. {"name": "Standard_LRS"}).
type DiskSku struct {
	Name string `json:"name"`
}

// Disk replica la forma estándar de ARM para Microsoft.Compute/disks.
type Disk struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        DiskSku           `json:"sku"`
	Properties DiskProperties    `json:"properties"`
}

type DiskProperties struct {
	ProvisioningState string       `json:"provisioningState"`
	DiskSizeGB        int          `json:"diskSizeGB"`
	DiskState         string       `json:"diskState"`
	CreationData      CreationData `json:"creationData"`
}

// CreationData replica "properties.creationData", el campo que ARM usa
// para describir el origen de un disco (vacío, desde imagen, desde otro
// disco/snapshot, ...). Solo se soporta "Empty" y "FromImage" aquí, que es
// lo único que azurerm_managed_disk / el osDisk de una VM necesitan.
type CreationData struct {
	CreateOption string `json:"createOption"`
}

type diskRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        DiskSku           `json:"sku"`
	Properties struct {
		DiskSizeGB   int          `json:"diskSizeGB"`
		CreationData CreationData `json:"creationData"`
	} `json:"properties"`
}

func (s *Service) registerDisks(mux *http.ServeMux) {
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks",
		s.listDisks)
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks/{diskName}",
		s.putDisk)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks/{diskName}",
		s.getDisk)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks/{diskName}",
		s.deleteDisk)
}

func diskKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func diskID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/disks/%s", subID, rg, name)
}

func (s *Service) putDisk(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("diskName")

	var req diskRequest
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
	createOption := req.Properties.CreationData.CreateOption
	if strings.TrimSpace(createOption) == "" {
		createOption = "Empty"
	}
	if createOption == "Empty" && req.Properties.DiskSizeGB <= 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.diskSizeGB' es obligatorio (>0) cuando createOption es 'Empty'")
		return
	}
	sku := req.Sku
	if strings.TrimSpace(sku.Name) == "" {
		sku.Name = "Standard_LRS"
	}

	key := diskKey(subID, rg, name)
	_, found, err := s.getDiskResource(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	diskSizeGB := req.Properties.DiskSizeGB
	if diskSizeGB <= 0 {
		diskSizeGB = 30 // tamaño por defecto razonable cuando createOption no es "Empty"
	}

	disk := Disk{
		ID:       diskID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Compute/disks",
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      sku,
		Properties: DiskProperties{
			ProvisioningState: "Succeeded",
			DiskSizeGB:        diskSizeGB,
			DiskState:         "Unattached",
			CreationData:      CreationData{CreateOption: createOption},
		},
	}

	if err := s.db.Put(disksBucket, key, disk); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, disk)
}

func (s *Service) getDisk(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("diskName")

	disk, found, err := s.getDiskResource(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el managed disk '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, disk)
}

func (s *Service) listDisks(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	disks := make([]Disk, 0)
	err := s.db.List(disksBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var disk Disk
		if err := json.Unmarshal(raw, &disk); err != nil {
			return err
		}
		disks = append(disks, disk)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": disks})
}

func (s *Service) deleteDisk(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("diskName")
	key := diskKey(subID, rg, name)

	found, err := s.db.Get(disksBucket, key, &Disk{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(disksBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) getDiskResource(subID, rg, name string) (Disk, bool, error) {
	var disk Disk
	found, err := s.db.Get(disksBucket, diskKey(subID, rg, name), &disk)
	return disk, found, err
}
