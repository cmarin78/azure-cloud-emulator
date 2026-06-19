package network

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const publicIPAddressesBucket = "network.publicips"

// PublicIPAddress replica Microsoft.Network/publicIPAddresses. A diferencia
// de Azure real (donde una IP "Dynamic" solo se asigna al asociarse a un
// recurso), aquí se asigna una IP determinista en el momento del PUT para
// simplificar: no hay un ciclo de vida de "unassigned" que el resto del
// emulador necesite modelar.
type PublicIPAddress struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Type       string                    `json:"type"`
	Location   string                    `json:"location"`
	Tags       map[string]string         `json:"tags,omitempty"`
	SKU        PublicIPAddressSKU        `json:"sku"`
	Properties PublicIPAddressProperties `json:"properties"`
}

type PublicIPAddressSKU struct {
	Name string `json:"name"`
}

type PublicIPAddressProperties struct {
	ProvisioningState        string `json:"provisioningState"`
	PublicIPAllocationMethod string `json:"publicIPAllocationMethod"`
	PublicIPAddressVersion   string `json:"publicIPAddressVersion"`
	IPAddress                string `json:"ipAddress,omitempty"`
}

type publicIPAddressRequest struct {
	Location   string             `json:"location"`
	Tags       map[string]string  `json:"tags,omitempty"`
	SKU        PublicIPAddressSKU `json:"sku"`
	Properties struct {
		PublicIPAllocationMethod string `json:"publicIPAllocationMethod"`
		PublicIPAddressVersion   string `json:"publicIPAddressVersion"`
	} `json:"properties"`
}

func (s *Service) registerPublicIPs(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/publicIPAddresses"
	mux.HandleFunc("GET "+base, s.listPublicIPs)
	mux.HandleFunc("PUT "+base+"/{publicIPName}", s.putPublicIP)
	mux.HandleFunc("GET "+base+"/{publicIPName}", s.getPublicIPHandler)
	mux.HandleFunc("DELETE "+base+"/{publicIPName}", s.deletePublicIP)
}

func publicIPKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func publicIPID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/publicIPAddresses/%s", subID, rg, name)
}

// fakePublicIP deriva una IP pública "creíble" (rango 20.x.x.x, similar al
// que usa Azure para IPs públicas reales) de forma determinista a partir
// del nombre completo del recurso, igual de espíritu que allocateIP en
// nic.go pero sin necesidad de una subred/CIDR de referencia.
func fakePublicIP(seed string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum32()
	b2 := (sum >> 16) & 0xFF
	b3 := (sum >> 8) & 0xFF
	b4 := sum & 0xFF
	if b2 == 0 {
		b2 = 1
	}
	return fmt.Sprintf("20.%d.%d.%d", b2, b3, b4)
}

func (s *Service) putPublicIP(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("publicIPName")

	var req publicIPAddressRequest
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

	skuName := req.SKU.Name
	if strings.TrimSpace(skuName) == "" {
		skuName = "Basic"
	}
	allocationMethod := req.Properties.PublicIPAllocationMethod
	if strings.TrimSpace(allocationMethod) == "" {
		allocationMethod = "Dynamic"
	}
	if allocationMethod != "Static" && allocationMethod != "Dynamic" {
		server.WriteError(w, http.StatusBadRequest, "InvalidParameter",
			"el campo 'properties.publicIPAllocationMethod' debe ser 'Static' o 'Dynamic'")
		return
	}
	version := req.Properties.PublicIPAddressVersion
	if strings.TrimSpace(version) == "" {
		version = "IPv4"
	}

	key := publicIPKey(subID, rg, name)
	var existing PublicIPAddress
	found, err := s.db.Get(publicIPAddressesBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	id := publicIPID(subID, rg, name)
	ip := existing.Properties.IPAddress
	if ip == "" {
		ip = fakePublicIP(id)
	}

	pip := PublicIPAddress{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.Network/publicIPAddresses",
		Location: req.Location,
		Tags:     req.Tags,
		SKU:      PublicIPAddressSKU{Name: skuName},
		Properties: PublicIPAddressProperties{
			ProvisioningState:        "Succeeded",
			PublicIPAllocationMethod: allocationMethod,
			PublicIPAddressVersion:   version,
			IPAddress:                ip,
		},
	}

	if err := s.db.Put(publicIPAddressesBucket, key, pip); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, pip)
}

// getPublicIP es el helper interno usado por loadbalancer.go para resolver
// referencias frontendIPConfiguration.publicIPAddress.id sin imponer
// integridad referencial estricta (mismo patrón que findSubnetByID).
func (s *Service) getPublicIP(subID, rg, name string) (PublicIPAddress, bool, error) {
	var pip PublicIPAddress
	found, err := s.db.Get(publicIPAddressesBucket, publicIPKey(subID, rg, name), &pip)
	return pip, found, err
}

func (s *Service) getPublicIPHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("publicIPName")

	pip, found, err := s.getPublicIP(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la public IP address '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, pip)
}

func (s *Service) listPublicIPs(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	pips := make([]PublicIPAddress, 0)
	err := s.db.List(publicIPAddressesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var pip PublicIPAddress
		if err := json.Unmarshal(raw, &pip); err != nil {
			return err
		}
		pips = append(pips, pip)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": pips})
}

func (s *Service) deletePublicIP(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("publicIPName")
	key := publicIPKey(subID, rg, name)

	found, err := s.db.Get(publicIPAddressesBucket, key, &PublicIPAddress{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(publicIPAddressesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
