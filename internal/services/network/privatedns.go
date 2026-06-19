package network

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// Microsoft.Network/privateDnsZones se modela dentro del mismo paquete
// network (en vez de un paquete internal/services/privatedns separado)
// porque comparte namespace ARM (Microsoft.Network), convenciones de
// bucket/key/CRUD síncrono, y no tiene tamaño suficiente para justificar
// un Service propio: es exactamente el mismo patrón "recurso padre +
// sub-recurso anidado" que VNet/Subnet y NSG/SecurityRule.
const privateDNSZonesBucket = "network.privatednszones"

// PrivateDNSZone replica Microsoft.Network/privateDnsZones. A diferencia
// de la mayoría de recursos de este paquete, en Azure real la location
// siempre es "global" (la zona no vive en una región), así que aquí se
// fuerza ese valor en vez de leerlo del request, igual que se hace con
// location="global" en monitor/actiongroups.go.
type PrivateDNSZone struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Location   string                   `json:"location"`
	Tags       map[string]string        `json:"tags,omitempty"`
	Properties PrivateDNSZoneProperties `json:"properties"`
}

type PrivateDNSZoneProperties struct {
	ProvisioningState  string `json:"provisioningState"`
	NumberOfRecordSets int    `json:"numberOfRecordSets"`
}

type privateDNSZoneRequest struct {
	Tags map[string]string `json:"tags,omitempty"`
}

// PrivateDNSRecordSet replica un record set de Microsoft.Network/
// privateDnsZones/{recordType} (A, AAAA, CNAME, MX, TXT, SRV, PTR...). ARM
// modela cada tipo de registro como su propio resourceType anidado bajo la
// zona (.../privateDnsZones/{zone}/A/{name}, .../CNAME/{name}, etc.), así
// que aquí "recordType" se captura como segmento de ruta variable en vez
// de un literal fijo por tipo, evitando duplicar handlers por cada tipo
// soportado.
type PrivateDNSRecordSet struct {
	ID         string                        `json:"id"`
	Name       string                        `json:"name"`
	Type       string                        `json:"type"`
	Properties PrivateDNSRecordSetProperties `json:"properties"`
}

type PrivateDNSRecordSetProperties struct {
	TTL         int64        `json:"ttl"`
	ARecords    []ARecord    `json:"aRecords,omitempty"`
	CnameRecord *CnameRecord `json:"cnameRecord,omitempty"`
	TxtRecords  []TxtRecord  `json:"txtRecords,omitempty"`
}

type ARecord struct {
	Ipv4Address string `json:"ipv4Address"`
}

type CnameRecord struct {
	Cname string `json:"cname"`
}

type TxtRecord struct {
	Value []string `json:"value"`
}

type privateDNSRecordSetRequest struct {
	Properties struct {
		TTL         int64        `json:"ttl"`
		ARecords    []ARecord    `json:"aRecords,omitempty"`
		CnameRecord *CnameRecord `json:"cnameRecord,omitempty"`
		TxtRecords  []TxtRecord  `json:"txtRecords,omitempty"`
	} `json:"properties"`
}

// privateDNSZoneRecord es la forma de persistencia interna: la zona más
// sus record sets, ya que Properties.RecordSets se excluye del JSON de
// respuesta (los record sets ARM reales no se devuelven inline en el GET
// de la zona, se consultan por separado vía su propio sub-recurso).
type privateDNSZoneRecord struct {
	Zone       PrivateDNSZone
	RecordSets []PrivateDNSRecordSet
}

func (s *Service) registerPrivateDNS(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/privateDnsZones"
	mux.HandleFunc("GET "+base, s.listPrivateDNSZones)
	mux.HandleFunc("PUT "+base+"/{zoneName}", s.putPrivateDNSZone)
	mux.HandleFunc("GET "+base+"/{zoneName}", s.getPrivateDNSZoneHandler)
	mux.HandleFunc("DELETE "+base+"/{zoneName}", s.deletePrivateDNSZone)

	mux.HandleFunc("GET "+base+"/{zoneName}/{recordType}", s.listPrivateDNSRecordSets)
	mux.HandleFunc("PUT "+base+"/{zoneName}/{recordType}/{recordName}", s.putPrivateDNSRecordSet)
	mux.HandleFunc("GET "+base+"/{zoneName}/{recordType}/{recordName}", s.getPrivateDNSRecordSet)
	mux.HandleFunc("DELETE "+base+"/{zoneName}/{recordType}/{recordName}", s.deletePrivateDNSRecordSet)
}

func privateDNSZoneKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func privateDNSZoneID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/privateDnsZones/%s", subID, rg, name)
}

func (s *Service) putPrivateDNSZone(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("zoneName")

	var req privateDNSZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	key := privateDNSZoneKey(subID, rg, name)
	existing, found, err := s.getPrivateDNSZoneRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	recordSets := make([]PrivateDNSRecordSet, 0)
	if found {
		recordSets = existing.RecordSets
	}

	rec := privateDNSZoneRecord{
		Zone: PrivateDNSZone{
			ID:       privateDNSZoneID(subID, rg, name),
			Name:     name,
			Type:     "Microsoft.Network/privateDnsZones",
			Location: "global",
			Tags:     req.Tags,
			Properties: PrivateDNSZoneProperties{
				ProvisioningState:  "Succeeded",
				NumberOfRecordSets: len(recordSets),
			},
		},
		RecordSets: recordSets,
	}

	if err := s.db.Put(privateDNSZonesBucket, key, rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, rec.Zone)
}

func (s *Service) getPrivateDNSZoneRecord(subID, rg, name string) (privateDNSZoneRecord, bool, error) {
	var rec privateDNSZoneRecord
	found, err := s.db.Get(privateDNSZonesBucket, privateDNSZoneKey(subID, rg, name), &rec)
	return rec, found, err
}

func (s *Service) getPrivateDNSZoneHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("zoneName")

	rec, found, err := s.getPrivateDNSZoneRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la private DNS zone '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, rec.Zone)
}

func (s *Service) listPrivateDNSZones(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	zones := make([]PrivateDNSZone, 0)
	err := s.db.List(privateDNSZonesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var rec privateDNSZoneRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		zones = append(zones, rec.Zone)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": zones})
}

func (s *Service) deletePrivateDNSZone(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("zoneName")
	key := privateDNSZoneKey(subID, rg, name)

	found, err := s.db.Get(privateDNSZonesBucket, key, &privateDNSZoneRecord{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(privateDNSZonesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) putPrivateDNSRecordSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	zoneName := r.PathValue("zoneName")
	recordType := r.PathValue("recordType")
	recordName := r.PathValue("recordName")

	var req privateDNSRecordSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if req.Properties.TTL <= 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.ttl' es obligatorio y debe ser mayor que cero")
		return
	}

	rec, found, err := s.getPrivateDNSZoneRecord(subID, rg, zoneName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la private DNS zone '%s' no existe en el resource group '%s'", zoneName, rg))
		return
	}

	recordSet := PrivateDNSRecordSet{
		ID:   fmt.Sprintf("%s/%s/%s", rec.Zone.ID, recordType, recordName),
		Name: recordName,
		Type: "Microsoft.Network/privateDnsZones/" + recordType,
		Properties: PrivateDNSRecordSetProperties{
			TTL:         req.Properties.TTL,
			ARecords:    req.Properties.ARecords,
			CnameRecord: req.Properties.CnameRecord,
			TxtRecords:  req.Properties.TxtRecords,
		},
	}

	existedBefore := false
	replaced := false
	for i, existingRS := range rec.RecordSets {
		if existingRS.Name == recordName && strings.EqualFold(existingRS.Type, recordSet.Type) {
			rec.RecordSets[i] = recordSet
			replaced = true
			existedBefore = true
			break
		}
	}
	if !replaced {
		rec.RecordSets = append(rec.RecordSets, recordSet)
	}
	rec.Zone.Properties.NumberOfRecordSets = len(rec.RecordSets)

	if err := s.db.Put(privateDNSZonesBucket, privateDNSZoneKey(subID, rg, zoneName), rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, recordSet)
}

func (s *Service) getPrivateDNSRecordSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	zoneName := r.PathValue("zoneName")
	recordType := r.PathValue("recordType")
	recordName := r.PathValue("recordName")

	rec, found, err := s.getPrivateDNSZoneRecord(subID, rg, zoneName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la private DNS zone '%s' no existe en el resource group '%s'", zoneName, rg))
		return
	}
	wantType := "Microsoft.Network/privateDnsZones/" + recordType
	for _, rs := range rec.RecordSets {
		if rs.Name == recordName && strings.EqualFold(rs.Type, wantType) {
			server.WriteJSON(w, http.StatusOK, rs)
			return
		}
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("el record set '%s/%s' no existe en la private DNS zone '%s'", recordType, recordName, zoneName))
}

func (s *Service) listPrivateDNSRecordSets(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	zoneName := r.PathValue("zoneName")
	recordType := r.PathValue("recordType")

	rec, found, err := s.getPrivateDNSZoneRecord(subID, rg, zoneName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la private DNS zone '%s' no existe en el resource group '%s'", zoneName, rg))
		return
	}
	wantType := "Microsoft.Network/privateDnsZones/" + recordType
	matches := make([]PrivateDNSRecordSet, 0)
	for _, rs := range rec.RecordSets {
		if strings.EqualFold(rs.Type, wantType) {
			matches = append(matches, rs)
		}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": matches})
}

func (s *Service) deletePrivateDNSRecordSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	zoneName := r.PathValue("zoneName")
	recordType := r.PathValue("recordType")
	recordName := r.PathValue("recordName")

	rec, found, err := s.getPrivateDNSZoneRecord(subID, rg, zoneName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	wantType := "Microsoft.Network/privateDnsZones/" + recordType
	kept := make([]PrivateDNSRecordSet, 0, len(rec.RecordSets))
	removed := false
	for _, rs := range rec.RecordSets {
		if rs.Name == recordName && strings.EqualFold(rs.Type, wantType) {
			removed = true
			continue
		}
		kept = append(kept, rs)
	}
	if !removed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rec.RecordSets = kept
	rec.Zone.Properties.NumberOfRecordSets = len(kept)

	if err := s.db.Put(privateDNSZonesBucket, privateDNSZoneKey(subID, rg, zoneName), rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
