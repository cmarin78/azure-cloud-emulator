// Package table emula el data plane de Azure Table Storage: tablas y
// entidades dentro de una storage account.
//
// Sigue el mismo patrón que internal/services/blob y
// internal/services/queue (ver el comentario de blob.go para el
// razonamiento completo): el data plane real de Azure vive en
// https://{account}.table.core.windows.net/..., y como este emulador no
// tiene un host por cuenta, storageaccounts.go ya devuelve endpoints
// "path-style" (http://{emulador}/{account}.table/...) — este paquete
// sirve ese shape, con "{account}.table" como primer segmento del path en
// vez de como host.
//
// Las mutaciones son síncronas (New(db), sin LRO/ops): crear una tabla o
// insertar/actualizar/borrar una entidad no es una operación de larga
// duración en Azure real.
//
// Diferencia clave con blob/queue: la API real de Table Storage ya usa
// JSON nativo (OData, con varios niveles de metadata posibles vía el
// header Accept) en vez de XML, así que aquí no hace falta la
// simplificación "XML real → JSON" que sí aplican blob.go/queue.go — el
// JSON que devolvemos es un subconjunto de lo que devolvería la API real
// con "Accept: application/json;odata=nometadata" (sin namespaces OData,
// sin tipos de propiedad explícitos vía "Edm.*@odata.type"). Tampoco se
// implementa autenticación AAD/SharedKey todavía (ver ROADMAP.md), así
// que SDKs/CLIs reales no pueden apuntar aquí sin parches.
//
// Shape de URLs soportado (subconjunto deliberado de la API real; ver
// cada handler para las simplificaciones puntuales):
//
//	GET    /{account}.table/Tables                                              → listar tablas
//	POST   /{account}.table/Tables                                              → crear tabla (body {"TableName": "..."})
//	DELETE /{account}.table/Tables('{table}')                                   → borrar tabla (+ sus entidades)
//	POST   /{account}.table/{table}                                             → insertar entidad (body con PartitionKey/RowKey)
//	GET    /{account}.table/{table}()[?$filter=...]                             → query de entidades (subconjunto de OData $filter)
//	GET    /{account}.table/{table}(PartitionKey='pk',RowKey='rk')              → obtener una entidad
//	PUT    /{account}.table/{table}(PartitionKey='pk',RowKey='rk')              → insert-or-replace (reemplaza todas las propiedades)
//	MERGE|PATCH /{account}.table/{table}(PartitionKey='pk',RowKey='rk')         → insert-or-merge (solo actualiza/agrega las propiedades enviadas)
//	DELETE /{account}.table/{table}(PartitionKey='pk',RowKey='rk')              → borrar entidad (If-Match opcional: '*' o el etag exacto)
package table

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const tablesBucket = "table.tables"
const entitiesBucket = "table.entities"

// entityKeyRe reconoce el shape "{table}(PartitionKey='pk',RowKey='rk')"
// usado por la API real para direccionar una entidad puntual (y también
// para la query de colección, que usa "{table}()" sin las claves).
var entityKeyRe = regexp.MustCompile(`^(.+)\(PartitionKey='([^']*)',RowKey='([^']*)'\)$`)

// tableDeleteRe reconoce el shape "Tables('{table}')" usado para borrar una
// tabla por nombre (la API real también lo usa para Get Table, que aquí no
// implementamos por separado ya que listar ya expone la misma info).
var tableDeleteRe = regexp.MustCompile(`^Tables\('([^']*)'\)$`)

// filterPartitionRe / filterRowRe reconocen el subconjunto de OData
// $filter que soportamos: igualdad exacta sobre PartitionKey y/o RowKey
// combinadas con "and". No hay un evaluador general de expresiones OData
// (eq/ne/gt/lt, paréntesis, comparación sobre propiedades custom, etc.) —
// alcanza para los usos más comunes de azurerm/SDKs (point queries y
// "todas las entidades de una partición"), que es lo que cubre el
// esfuerzo "S" de este servicio en ROADMAP.md.
var filterPartitionRe = regexp.MustCompile(`PartitionKey\s+eq\s+'([^']*)'`)
var filterRowRe = regexp.MustCompile(`RowKey\s+eq\s+'([^']*)'`)

// Service agrupa el estado necesario para atender las rutas de data plane
// de Table Storage.
type Service struct {
	db *storage.DB
}

// New crea el servicio de tablas/entidades.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Table replica el subconjunto relevante de metadata de una tabla.
type Table struct {
	Name         string `json:"TableName"`
	Account      string `json:"accountName"`
	LastModified string `json:"lastModified"`
}

// storedEntity es la forma de persistencia de una entidad: las propiedades
// "de sistema" (PartitionKey/RowKey/Timestamp/ETag) van en campos propios
// para poder indexar/filtrar sin tener que parsear el resto del mapa, y
// las propiedades de negocio del cliente quedan en Properties tal cual
// las mandó (cualquier tipo JSON válido: string, number, bool).
type storedEntity struct {
	PartitionKey string         `json:"partitionKey"`
	RowKey       string         `json:"rowKey"`
	Timestamp    string         `json:"timestamp"`
	ETag         string         `json:"etag"`
	Properties   map[string]any `json:"properties"`
}

// toResponse aplana una storedEntity al shape JSON que expone la API real
// con "odata=nometadata" más un campo "odata.etag" (la API real solo
// incluye ese campo con metadata=minimal/full, pero lo dejamos siempre
// presente para que los smoke tests puedan leer el etag sin tener que
// negociar el header Accept, que este emulador no implementa).
func toResponse(e storedEntity) map[string]any {
	out := make(map[string]any, len(e.Properties)+4)
	for k, v := range e.Properties {
		out[k] = v
	}
	out["PartitionKey"] = e.PartitionKey
	out["RowKey"] = e.RowKey
	out["Timestamp"] = e.Timestamp
	out["odata.etag"] = e.ETag
	return out
}

// ServeHTTP atiende una request de data plane de tablas ya enrutada por el
// dispatcher compartido (ver cmd/azure-emulator/main.go y el comentario de
// blob.Service.ServeHTTP: blob/queue/table no pueden registrar cada uno su
// propio "/{accountX}/{path...}" en el mux porque net/http.ServeMux trata
// todos esos patrones como la misma forma de ruta sin importar el nombre
// del wildcard, así que un único dispatcher central registra el patrón una
// vez y despacha por sufijo ".blob"/".queue"/".table" del primer segmento).
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountTable := r.PathValue("accountResource")
	account, ok := strings.CutSuffix(accountTable, ".table")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{account}.table/...'")
		return
	}

	// A diferencia de blob/queue, el primer segmento del path bajo la
	// cuenta puede contener "/" propio si en el futuro soportáramos rutas
	// anidadas, pero la API real de Table Storage no las tiene: todo vive
	// en un único segmento ("Tables", "Tables('x')", "{table}",
	// "{table}()" o "{table}(PartitionKey=...,RowKey=...)"), así que basta
	// con el primer componente de "path".
	seg := strings.Trim(r.PathValue("path"), "/")
	seg, _ = strings.CutSuffix(seg, "/") // tolera una barra final accidental

	switch {
	case seg == "Tables":
		s.handleTables(w, r, account)
	case strings.HasPrefix(seg, "Tables("):
		s.handleTableByName(w, r, account, seg)
	case seg == "":
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta inválida: se esperaba 'Tables', 'Tables(...)' o '{table}[(...)]'")
	default:
		s.handleEntityRoute(w, r, account, seg)
	}
}

func tableKey(account, name string) string {
	return account + "/" + name
}

func entityPrefix(account, table string) string {
	return account + "/" + table + "/"
}

func entityKey(account, table, partitionKey, rowKey string) string {
	return account + "/" + table + "/" + partitionKey + "/" + rowKey
}

// handleTables atiende la colección de tablas de la cuenta: listar (GET) y
// crear (POST), igual que "Query Tables"/"Create Table" en la API real.
func (s *Service) handleTables(w http.ResponseWriter, r *http.Request, account string) {
	switch r.Method {
	case http.MethodGet:
		s.listTables(w, r, account)
	case http.MethodPost:
		s.createTable(w, r, account)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para la colección de tablas")
	}
}

func (s *Service) listTables(w http.ResponseWriter, r *http.Request, account string) {
	tables := make([]Table, 0)
	err := s.db.List(tablesBucket, account+"/", func(key string, raw []byte) error {
		var t Table
		if err := json.Unmarshal(raw, &t); err != nil {
			return err
		}
		tables = append(tables, t)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": tables})
}

// createTable espera el body real de "Create Table":
// {"TableName": "nombre"}.
func (s *Service) createTable(w http.ResponseWriter, r *http.Request, account string) {
	var body struct {
		TableName string `json:"TableName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput", "body inválido: se esperaba {\"TableName\": \"...\"}")
		return
	}
	name := strings.TrimSpace(body.TableName)
	if name == "" {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput", "TableName no puede estar vacío")
		return
	}

	key := tableKey(account, name)
	found, err := s.db.Get(tablesBucket, key, &Table{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if found {
		server.WriteError(w, http.StatusConflict, "TableAlreadyExists",
			fmt.Sprintf("la tabla '%s' ya existe en la cuenta '%s'", name, account))
		return
	}

	t := Table{
		Name:         name,
		Account:      account,
		LastModified: time.Now().UTC().Format(time.RFC1123),
	}
	if err := s.db.Put(tablesBucket, key, t); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, t)
}

// handleTableByName atiende "Tables('{table}')": solo DELETE en este
// emulador (la API real también soporta GET de una tabla individual, pero
// listTables ya expone la misma información y no aporta cobertura nueva
// para los smoke tests de este proyecto).
func (s *Service) handleTableByName(w http.ResponseWriter, r *http.Request, account, seg string) {
	m := tableDeleteRe.FindStringSubmatch(seg)
	if m == nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput",
			"se esperaba el shape \"Tables('{table}')\"")
		return
	}
	if r.Method != http.MethodDelete {
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para una tabla individual (solo DELETE)")
		return
	}
	s.deleteTable(w, r, account, m[1])
}

// deleteTable también borra en cascada todas las entidades de la tabla,
// igual que deleteContainer en blob.go / deleteQueue en queue.go.
func (s *Service) deleteTable(w http.ResponseWriter, r *http.Request, account, name string) {
	key := tableKey(account, name)
	found, err := s.db.Get(tablesBucket, key, &Table{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "TableNotFound",
			fmt.Sprintf("la tabla '%s' no existe en la cuenta '%s'", name, account))
		return
	}

	var keysToDelete []string
	err = s.db.List(entitiesBucket, entityPrefix(account, name), func(k string, _ []byte) error {
		keysToDelete = append(keysToDelete, k)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	for _, k := range keysToDelete {
		if err := s.db.Delete(entitiesBucket, k); err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}
	if err := s.db.Delete(tablesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEntityRoute despacha todo lo relacionado a entidades de una tabla:
// el segmento puede ser "{table}" (insert), "{table}()" (query, con o sin
// "()") o "{table}(PartitionKey='pk',RowKey='rk')" (entidad puntual).
func (s *Service) handleEntityRoute(w http.ResponseWriter, r *http.Request, account, seg string) {
	if m := entityKeyRe.FindStringSubmatch(seg); m != nil {
		table, pk, rk := m[1], m[2], m[3]
		if !s.tableExists(w, account, table) {
			return
		}
		s.handleEntity(w, r, account, table, pk, rk)
		return
	}

	table := strings.TrimSuffix(seg, "()")
	if !s.tableExists(w, account, table) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.insertEntity(w, r, account, table)
	case http.MethodGet:
		s.queryEntities(w, r, account, table)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para la colección de entidades")
	}
}

// tableExists escribe la respuesta de error 404 y devuelve false si la
// tabla no existe; los handlers de entidades llaman esto primero para no
// repetir la misma comprobación en cada operación (mismo patrón que
// queueExists en queue.go).
func (s *Service) tableExists(w http.ResponseWriter, account, table string) bool {
	found, err := s.db.Get(tablesBucket, tableKey(account, table), &Table{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return false
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "TableNotFound",
			fmt.Sprintf("la tabla '%s' no existe en la cuenta '%s'", table, account))
		return false
	}
	return true
}

// insertEntity atiende "Insert Entity" (POST a la colección, sin claves en
// la URL): el body debe incluir PartitionKey y RowKey junto al resto de
// propiedades de negocio.
func (s *Service) insertEntity(w http.ResponseWriter, r *http.Request, account, table string) {
	props, pk, rk, err := decodeEntityBody(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput", err.Error())
		return
	}
	if pk == "" || rk == "" {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput",
			"el body debe incluir PartitionKey y RowKey no vacíos")
		return
	}

	key := entityKey(account, table, pk, rk)
	found, err := s.db.Get(entitiesBucket, key, &storedEntity{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if found {
		server.WriteError(w, http.StatusConflict, "EntityAlreadyExists",
			fmt.Sprintf("la entidad PartitionKey='%s'/RowKey='%s' ya existe en la tabla '%s'", pk, rk, table))
		return
	}

	rec := storedEntity{
		PartitionKey: pk,
		RowKey:       rk,
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		ETag:         etagNow(),
		Properties:   props,
	}
	if err := s.db.Put(entitiesBucket, key, rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("ETag", rec.ETag)
	server.WriteJSON(w, http.StatusCreated, toResponse(rec))
}

// queryEntities atiende "Query Entities" (GET sobre la colección, con o
// sin "()"). Soporta el subconjunto de $filter documentado en
// filterPartitionRe/filterRowRe; cualquier otra expresión se ignora (se
// devuelven todas las entidades de la tabla) en vez de fallar, ya que un
// evaluador OData completo está fuera del alcance "S" de este servicio.
func (s *Service) queryEntities(w http.ResponseWriter, r *http.Request, account, table string) {
	filter := r.URL.Query().Get("$filter")
	var wantPartition, wantRow string
	var hasPartition, hasRow bool
	if filter != "" {
		if m := filterPartitionRe.FindStringSubmatch(filter); m != nil {
			wantPartition, hasPartition = m[1], true
		}
		if m := filterRowRe.FindStringSubmatch(filter); m != nil {
			wantRow, hasRow = m[1], true
		}
	}

	results := make([]map[string]any, 0)
	err := s.db.List(entitiesBucket, entityPrefix(account, table), func(_ string, raw []byte) error {
		var rec storedEntity
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		if hasPartition && rec.PartitionKey != wantPartition {
			return nil
		}
		if hasRow && rec.RowKey != wantRow {
			return nil
		}
		results = append(results, toResponse(rec))
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": results})
}

// handleEntity despacha las operaciones sobre una entidad puntual
// (dirección "{table}(PartitionKey='pk',RowKey='rk')"): get, insert-or-
// replace (PUT), insert-or-merge (MERGE/PATCH) y delete.
func (s *Service) handleEntity(w http.ResponseWriter, r *http.Request, account, table, pk, rk string) {
	switch r.Method {
	case http.MethodGet:
		s.getEntity(w, r, account, table, pk, rk)
	case http.MethodPut:
		s.upsertEntity(w, r, account, table, pk, rk, false)
	case "MERGE", http.MethodPatch:
		s.upsertEntity(w, r, account, table, pk, rk, true)
	case http.MethodDelete:
		s.deleteEntity(w, r, account, table, pk, rk)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para una entidad individual")
	}
}

func (s *Service) getEntity(w http.ResponseWriter, r *http.Request, account, table, pk, rk string) {
	var rec storedEntity
	found, err := s.db.Get(entitiesBucket, entityKey(account, table, pk, rk), &rec)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "EntityNotFound",
			fmt.Sprintf("la entidad PartitionKey='%s'/RowKey='%s' no existe en la tabla '%s'", pk, rk, table))
		return
	}
	w.Header().Set("ETag", rec.ETag)
	server.WriteJSON(w, http.StatusOK, toResponse(rec))
}

// upsertEntity implementa tanto "Insert Or Replace Entity" (PUT, merge=false:
// reemplaza todas las propiedades de negocio) como "Insert Or Merge Entity"
// (MERGE/PATCH, merge=true: solo agrega/sobrescribe las propiedades
// presentes en el body, conservando las demás) — ambas son upsert en la API
// real, la única diferencia es qué pasa con las propiedades NO mencionadas
// en el body de una entidad ya existente.
//
// If-Match es opcional aquí (simplificación deliberada, igual de espíritu
// que blob/queue no implementar control de concurrencia obligatorio): si
// el cliente lo manda y no es "*" ni coincide con el etag actual, se
// devuelve 412; si no lo manda, se omite la comprobación.
func (s *Service) upsertEntity(w http.ResponseWriter, r *http.Request, account, table, pk, rk string, merge bool) {
	props, bodyPK, bodyRK, err := decodeEntityBody(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput", err.Error())
		return
	}
	if bodyPK != "" && bodyPK != pk || bodyRK != "" && bodyRK != rk {
		server.WriteError(w, http.StatusBadRequest, "InvalidInput",
			"PartitionKey/RowKey del body no coinciden con los de la URL")
		return
	}

	key := entityKey(account, table, pk, rk)
	var existing storedEntity
	found, err := s.db.Get(entitiesBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if found {
		if ifMatch := r.Header.Get("If-Match"); ifMatch != "" && ifMatch != "*" && ifMatch != existing.ETag {
			server.WriteError(w, http.StatusPreconditionFailed, "UpdateConditionNotSatisfied",
				"el If-Match no coincide con el etag actual de la entidad")
			return
		}
	}

	finalProps := props
	if merge && found {
		finalProps = make(map[string]any, len(existing.Properties)+len(props))
		for k, v := range existing.Properties {
			finalProps[k] = v
		}
		for k, v := range props {
			finalProps[k] = v
		}
	}

	rec := storedEntity{
		PartitionKey: pk,
		RowKey:       rk,
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		ETag:         etagNow(),
		Properties:   finalProps,
	}
	if err := s.db.Put(entitiesBucket, key, rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("ETag", rec.ETag)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) deleteEntity(w http.ResponseWriter, r *http.Request, account, table, pk, rk string) {
	key := entityKey(account, table, pk, rk)
	var existing storedEntity
	found, err := s.db.Get(entitiesBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "EntityNotFound",
			fmt.Sprintf("la entidad PartitionKey='%s'/RowKey='%s' no existe en la tabla '%s'", pk, rk, table))
		return
	}
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" && ifMatch != "*" && ifMatch != existing.ETag {
		server.WriteError(w, http.StatusPreconditionFailed, "UpdateConditionNotSatisfied",
			"el If-Match no coincide con el etag actual de la entidad")
		return
	}
	if err := s.db.Delete(entitiesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeEntityBody parsea el body JSON de una entidad y separa
// PartitionKey/RowKey (si están presentes) del resto de propiedades de
// negocio. Cuando se usa para insertEntity, pk/rk son obligatorios; los
// callers de upsert/merge validan por su cuenta si hace falta que estén
// vacíos o coincidan con la URL.
func decodeEntityBody(body io.Reader) (props map[string]any, pk, rk string, err error) {
	raw := map[string]any{}
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, "", "", fmt.Errorf("body inválido: se esperaba un objeto JSON con PartitionKey/RowKey y propiedades")
	}

	if v, ok := raw["PartitionKey"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, "", "", fmt.Errorf("PartitionKey debe ser un string")
		}
		pk = s
		delete(raw, "PartitionKey")
	}
	if v, ok := raw["RowKey"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, "", "", fmt.Errorf("RowKey debe ser un string")
		}
		rk = s
		delete(raw, "RowKey")
	}
	// Timestamp/odata.etag son campos de solo lectura que la API real
	// también ignora si el cliente los manda de vuelta (p. ej. al hacer un
	// roundtrip get→put); los descartamos en vez de fallar.
	delete(raw, "Timestamp")
	delete(raw, "odata.etag")

	return raw, pk, rk, nil
}

func etagNow() string {
	return fmt.Sprintf("%q", time.Now().UTC().Format(time.RFC3339Nano))
}
