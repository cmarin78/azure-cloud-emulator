package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const routeTablesBucket = "network.routetables"

// Route replica Microsoft.Network/routeTables/routes. Igual que las
// subnets en una VNet y las securityRules en un NSG, vive anidada en
// properties.routes pero también se expone como su propio sub-recurso ARM
// (.../routeTables/{rt}/routes/{route}), que es como azurerm_route las
// gestiona.
type Route struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Properties RouteProperties `json:"properties"`
}

type RouteProperties struct {
	ProvisioningState string `json:"provisioningState"`
	AddressPrefix     string `json:"addressPrefix"`
	NextHopType       string `json:"nextHopType"`
	NextHopIPAddress  string `json:"nextHopIpAddress,omitempty"`
}

// RouteTable replica Microsoft.Network/routeTables, incluyendo sus routes
// anidadas (mismo patrón que VirtualNetwork/Subnets en network.go).
type RouteTable struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Location   string               `json:"location"`
	Tags       map[string]string    `json:"tags,omitempty"`
	Properties RouteTableProperties `json:"properties"`
}

type RouteTableProperties struct {
	ProvisioningState          string  `json:"provisioningState"`
	DisableBGPRoutePropagation bool    `json:"disableBgpRoutePropagation"`
	Routes                     []Route `json:"routes"`
}

type routeTableRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		DisableBGPRoutePropagation bool `json:"disableBgpRoutePropagation"`
	} `json:"properties"`
}

type routeRequest struct {
	Properties struct {
		AddressPrefix    string `json:"addressPrefix"`
		NextHopType      string `json:"nextHopType"`
		NextHopIPAddress string `json:"nextHopIpAddress"`
	} `json:"properties"`
}

var validNextHopTypes = map[string]bool{
	"VirtualAppliance":      true,
	"VirtualNetworkGateway": true,
	"VnetLocal":             true,
	"Internet":              true,
	"None":                  true,
}

func (s *Service) registerRouteTables(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/routeTables"
	mux.HandleFunc("GET "+base, s.listRouteTables)
	mux.HandleFunc("PUT "+base+"/{rtName}", s.putRouteTable)
	mux.HandleFunc("GET "+base+"/{rtName}", s.getRouteTableHandler)
	mux.HandleFunc("DELETE "+base+"/{rtName}", s.deleteRouteTable)

	mux.HandleFunc("GET "+base+"/{rtName}/routes", s.listRoutes)
	mux.HandleFunc("PUT "+base+"/{rtName}/routes/{routeName}", s.putRoute)
	mux.HandleFunc("GET "+base+"/{rtName}/routes/{routeName}", s.getRoute)
	mux.HandleFunc("DELETE "+base+"/{rtName}/routes/{routeName}", s.deleteRoute)
}

func routeTableKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func routeTableID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/routeTables/%s", subID, rg, name)
}

func (s *Service) putRouteTable(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("rtName")

	var req routeTableRequest
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

	key := routeTableKey(subID, rg, name)
	existing, found, err := s.getRouteTable(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	rt := RouteTable{
		ID:       routeTableID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Network/routeTables",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: RouteTableProperties{
			ProvisioningState:          "Succeeded",
			DisableBGPRoutePropagation: req.Properties.DisableBGPRoutePropagation,
			Routes:                     make([]Route, 0),
		},
	}
	// Preservar las routes ya creadas vía el sub-recurso si esto es un
	// update de una route table existente (mismo enfoque que putVirtualNetwork
	// con sus subnets y putNSG con sus securityRules).
	if found {
		rt.Properties.Routes = existing.Properties.Routes
	}

	if err := s.db.Put(routeTablesBucket, key, rt); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rt)
}

func (s *Service) getRouteTable(subID, rg, name string) (RouteTable, bool, error) {
	var rt RouteTable
	found, err := s.db.Get(routeTablesBucket, routeTableKey(subID, rg, name), &rt)
	return rt, found, err
}

func (s *Service) getRouteTableHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("rtName")

	rt, found, err := s.getRouteTable(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la route table '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, rt)
}

func (s *Service) listRouteTables(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	rts := make([]RouteTable, 0)
	err := s.db.List(routeTablesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var rt RouteTable
		if err := json.Unmarshal(raw, &rt); err != nil {
			return err
		}
		rts = append(rts, rt)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": rts})
}

func (s *Service) deleteRouteTable(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("rtName")
	key := routeTableKey(subID, rg, name)

	found, err := s.db.Get(routeTablesBucket, key, &RouteTable{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(routeTablesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) putRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	rtName := r.PathValue("rtName")
	routeName := r.PathValue("routeName")

	var req routeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Properties.AddressPrefix) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.addressPrefix' es obligatorio")
		return
	}
	if !validNextHopTypes[req.Properties.NextHopType] {
		server.WriteError(w, http.StatusBadRequest, "InvalidParameter",
			"el campo 'properties.nextHopType' debe ser uno de: VirtualAppliance, VirtualNetworkGateway, VnetLocal, Internet, None")
		return
	}

	rt, found, err := s.getRouteTable(subID, rg, rtName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la route table '%s' no existe en el resource group '%s'", rtName, rg))
		return
	}

	route := Route{
		ID:   rt.ID + "/routes/" + routeName,
		Name: routeName,
		Type: "Microsoft.Network/routeTables/routes",
		Properties: RouteProperties{
			ProvisioningState: "Succeeded",
			AddressPrefix:     req.Properties.AddressPrefix,
			NextHopType:       req.Properties.NextHopType,
			NextHopIPAddress:  req.Properties.NextHopIPAddress,
		},
	}

	existedBefore := false
	replaced := false
	for i, existingRoute := range rt.Properties.Routes {
		if existingRoute.Name == routeName {
			rt.Properties.Routes[i] = route
			replaced = true
			existedBefore = true
			break
		}
	}
	if !replaced {
		rt.Properties.Routes = append(rt.Properties.Routes, route)
	}

	if err := s.db.Put(routeTablesBucket, routeTableKey(subID, rg, rtName), rt); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, route)
}

func (s *Service) getRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	rtName := r.PathValue("rtName")
	routeName := r.PathValue("routeName")

	rt, found, err := s.getRouteTable(subID, rg, rtName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la route table '%s' no existe en el resource group '%s'", rtName, rg))
		return
	}
	for _, route := range rt.Properties.Routes {
		if route.Name == routeName {
			server.WriteJSON(w, http.StatusOK, route)
			return
		}
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("la route '%s' no existe en la route table '%s'", routeName, rtName))
}

func (s *Service) listRoutes(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	rtName := r.PathValue("rtName")

	rt, found, err := s.getRouteTable(subID, rg, rtName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la route table '%s' no existe en el resource group '%s'", rtName, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": rt.Properties.Routes})
}

func (s *Service) deleteRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	rtName := r.PathValue("rtName")
	routeName := r.PathValue("routeName")

	rt, found, err := s.getRouteTable(subID, rg, rtName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	kept := make([]Route, 0, len(rt.Properties.Routes))
	removed := false
	for _, route := range rt.Properties.Routes {
		if route.Name == routeName {
			removed = true
			continue
		}
		kept = append(kept, route)
	}
	if !removed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rt.Properties.Routes = kept

	if err := s.db.Put(routeTablesBucket, routeTableKey(subID, rg, rtName), rt); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
