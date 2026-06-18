// Package blob emula el data plane de Azure Blob Storage: containers y
// blobs dentro de una storage account.
//
// A diferencia de internal/services/storageaccounts (control plane ARM,
// bajo /subscriptions/.../providers/Microsoft.Storage/...), el data plane
// real de Azure vive en un host completamente distinto:
// https://{account}.blob.core.windows.net/{container}/{blob}. Como este
// emulador no tiene un host por cuenta, storageaccounts.go ya devuelve
// endpoints "path-style" (http://{emulador}/{account}.blob/...) — este
// paquete sirve exactamente ese shape, con el nombre de cuenta + ".blob"
// como primer segmento del path en vez de como host.
//
// Igual que internal/services/storageaccounts, las mutaciones aquí son
// síncronas (a diferencia de crear una storage account, subir/borrar un
// blob en Azure real no es una operación de larga duración), así que no
// se necesita el helper de LRO (internal/server.Operations) — el mismo
// motivo por el que gcp-emulator's internal/services/gcs tampoco lo usa.
//
// Las respuestas de metadata (listar containers/blobs, propiedades) se
// devuelven en JSON en vez del XML que usa la API REST real de Azure: el
// emulador no implementa todavía autenticación AAD/SharedKey (ver
// ROADMAP.md), así que herramientas reales como az CLI/azure-storage-blob
// SDK no pueden apuntar aquí sin parches. JSON simple es suficiente para
// los smoke tests de este proyecto (az rest / curl / Invoke-RestMethod),
// igual que el resto del emulador.
package blob

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const containersBucket = "blob.containers"
const blobsBucket = "blob.blobs"

// Service agrupa el estado necesario para atender las rutas de data plane
// de blobs.
type Service struct {
	db *storage.DB
}

// New crea el servicio de blob containers/blobs.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Container replica el subconjunto relevante de metadata de un container
// de blobs.
type Container struct {
	Name         string `json:"name"`
	Account      string `json:"accountName"`
	LastModified string `json:"lastModified"`
}

// Blob replica el subconjunto relevante de metadata de un blob. El
// contenido real no viaja en esta struct (se sirve aparte vía GET sin
// comp=metadata) para no inflar las respuestas de "list blobs".
type Blob struct {
	Name          string `json:"name"`
	Container     string `json:"container"`
	ContentType   string `json:"contentType,omitempty"`
	ContentLength int64  `json:"contentLength"`
	LastModified  string `json:"lastModified"`
	ETag          string `json:"etag"`
}

// storedBlob añade el contenido (base64) a Blob solo para persistencia;
// nunca se serializa en respuestas de listado/metadata.
type storedBlob struct {
	Blob
	ContentB64 string `json:"contentB64"`
}

// ServeHTTP atiende una request de data plane de blobs ya enrutada por el
// dispatcher compartido (ver cmd/azure-emulator/main.go): ese dispatcher
// es el único que registra el patrón "/{accountResource}/{path...}" en el
// mux (net/http.ServeMux no permite registrar dos patrones con la misma
// forma de wildcards, así que blob y queue no pueden registrar cada uno
// su propio "/{accountX}/{path...}" — chocarían pese a tener nombres de
// wildcard distintos), y delega aquí cuando el primer segmento del path
// termina en ".blob". El wildcard final "{path...}" también matchea cero
// segmentos restantes (path=""), que es el caso de operaciones a nivel de
// cuenta (GET /{account}.blob/?comp=list).
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountBlob := r.PathValue("accountResource")
	account, ok := strings.CutSuffix(accountBlob, ".blob")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{account}.blob/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	var container, blobName string
	if rest != "" {
		parts := strings.SplitN(rest, "/", 2)
		container = parts[0]
		if len(parts) == 2 {
			blobName = parts[1]
		}
	}

	switch {
	case container == "":
		s.handleAccount(w, r, account)
	case blobName == "":
		s.handleContainer(w, r, account, container)
	default:
		s.handleBlob(w, r, account, container, blobName)
	}
}

func containerKey(account, container string) string {
	return account + "/" + container
}

func blobKey(account, container, name string) string {
	return account + "/" + container + "/" + name
}

// handleAccount atiende operaciones a nivel de cuenta: hoy solo
// "List Containers" (GET /{account}.blob/?comp=list), que es lo que az
// CLI/SDKs usan para descubrir containers existentes.
func (s *Service) handleAccount(w http.ResponseWriter, r *http.Request, account string) {
	if r.Method != http.MethodGet {
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"a nivel de cuenta solo se soporta GET (list containers)")
		return
	}
	if r.URL.Query().Get("comp") != "list" {
		server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"se esperaba '?comp=list' para listar containers de la cuenta")
		return
	}

	containers := make([]Container, 0)
	err := s.db.List(containersBucket, account+"/", func(key string, raw []byte) error {
		var c Container
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		containers = append(containers, c)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": containers})
}

// handleContainer despacha las operaciones de un container según el
// método HTTP. Igual que la API real, todas requieren '?restype=container'
// para distinguir "operación sobre el container" de "operación sobre un
// blob cuyo nombre coincide con el container" (caso degenerado, pero
// seguimos la convención real para no ambigüar el shape de la URL).
func (s *Service) handleContainer(w http.ResponseWriter, r *http.Request, account, container string) {
	if r.URL.Query().Get("restype") != "container" {
		server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"operaciones a nivel de container requieren '?restype=container'")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.createContainer(w, r, account, container)
	case http.MethodHead:
		s.headContainer(w, r, account, container)
	case http.MethodGet:
		if r.URL.Query().Get("comp") == "list" {
			s.listBlobs(w, r, account, container)
			return
		}
		s.getContainer(w, r, account, container)
	case http.MethodDelete:
		s.deleteContainer(w, r, account, container)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para containers")
	}
}

func (s *Service) createContainer(w http.ResponseWriter, r *http.Request, account, container string) {
	key := containerKey(account, container)
	var existing Container
	found, err := s.db.Get(containersBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if found {
		server.WriteError(w, http.StatusConflict, "ContainerAlreadyExists",
			fmt.Sprintf("el container '%s' ya existe en la cuenta '%s'", container, account))
		return
	}

	c := Container{
		Name:         container,
		Account:      account,
		LastModified: time.Now().UTC().Format(time.RFC1123),
	}
	if err := s.db.Put(containersBucket, key, c); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("ETag", etagNow())
	w.Header().Set("Last-Modified", c.LastModified)
	w.WriteHeader(http.StatusCreated)
}

func (s *Service) getContainer(w http.ResponseWriter, r *http.Request, account, container string) {
	var c Container
	found, err := s.db.Get(containersBucket, containerKey(account, container), &c)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ContainerNotFound",
			fmt.Sprintf("el container '%s' no existe en la cuenta '%s'", container, account))
		return
	}
	server.WriteJSON(w, http.StatusOK, c)
}

func (s *Service) headContainer(w http.ResponseWriter, r *http.Request, account, container string) {
	var c Container
	found, err := s.db.Get(containersBucket, containerKey(account, container), &c)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Last-Modified", c.LastModified)
	w.WriteHeader(http.StatusOK)
}

// deleteContainer también borra en cascada todos los blobs del container,
// igual que Azure real (que los marca para borrado asíncrono internamente,
// pero desde la perspectiva del cliente el container y su contenido
// desaparecen juntos).
func (s *Service) deleteContainer(w http.ResponseWriter, r *http.Request, account, container string) {
	key := containerKey(account, container)
	found, err := s.db.Get(containersBucket, key, &Container{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ContainerNotFound",
			fmt.Sprintf("el container '%s' no existe en la cuenta '%s'", container, account))
		return
	}

	var keysToDelete []string
	err = s.db.List(blobsBucket, account+"/"+container+"/", func(k string, _ []byte) error {
		keysToDelete = append(keysToDelete, k)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	for _, k := range keysToDelete {
		if err := s.db.Delete(blobsBucket, k); err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}
	if err := s.db.Delete(containersBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) listBlobs(w http.ResponseWriter, r *http.Request, account, container string) {
	found, err := s.db.Get(containersBucket, containerKey(account, container), &Container{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ContainerNotFound",
			fmt.Sprintf("el container '%s' no existe en la cuenta '%s'", container, account))
		return
	}

	prefix := r.URL.Query().Get("prefix")
	blobs := make([]Blob, 0)
	err = s.db.List(blobsBucket, account+"/"+container+"/"+prefix, func(key string, raw []byte) error {
		var rec storedBlob
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		blobs = append(blobs, rec.Blob)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": blobs})
}

// handleBlob despacha las operaciones sobre un blob individual. El nombre
// del blob puede incluir "/" (directorios virtuales), por eso viene ya
// resuelto como el resto del path después del nombre del container.
func (s *Service) handleBlob(w http.ResponseWriter, r *http.Request, account, container, name string) {
	switch r.Method {
	case http.MethodPut:
		s.putBlob(w, r, account, container, name)
	case http.MethodGet:
		s.readBlob(w, r, account, container, name, true)
	case http.MethodHead:
		s.readBlob(w, r, account, container, name, false)
	case http.MethodDelete:
		s.deleteBlob(w, r, account, container, name)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para blobs")
	}
}

func (s *Service) putBlob(w http.ResponseWriter, r *http.Request, account, container, name string) {
	found, err := s.db.Get(containersBucket, containerKey(account, container), &Container{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ContainerNotFound",
			fmt.Sprintf("el container '%s' no existe en la cuenta '%s'", container, account))
		return
	}

	// Azure real exige el header x-ms-blob-type (BlockBlob/PageBlob/
	// AppendBlob); aquí lo aceptamos pero no lo validamos estrictamente
	// (el emulador solo modela block blobs, que es el caso de uso de
	// azurerm_storage_blob / la inmensa mayoría de SDKs).
	data, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	contentType := r.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}

	now := time.Now().UTC().Format(time.RFC1123)
	etag := etagNow()
	rec := storedBlob{
		Blob: Blob{
			Name:          name,
			Container:     container,
			ContentType:   contentType,
			ContentLength: int64(len(data)),
			LastModified:  now,
			ETag:          etag,
		},
		ContentB64: base64.StdEncoding.EncodeToString(data),
	}
	if err := s.db.Put(blobsBucket, blobKey(account, container, name), rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", now)
	w.WriteHeader(http.StatusCreated)
}

// readBlob atiende tanto "Get Blob" (withBody=true, devuelve el contenido
// real) como "Get Blob Properties" (withBody=false, vía HEAD: mismos
// headers, sin cuerpo), igual que la API real de Azure.
func (s *Service) readBlob(w http.ResponseWriter, r *http.Request, account, container, name string, withBody bool) {
	var rec storedBlob
	found, err := s.db.Get(blobsBucket, blobKey(account, container, name), &rec)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "BlobNotFound",
			fmt.Sprintf("el blob '%s' no existe en el container '%s'", name, container))
		return
	}

	w.Header().Set("Content-Type", rec.ContentType)
	w.Header().Set("ETag", rec.ETag)
	w.Header().Set("Last-Modified", rec.LastModified)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", rec.ContentLength))

	if !withBody {
		w.WriteHeader(http.StatusOK)
		return
	}
	data, err := base64.StdEncoding.DecodeString(rec.ContentB64)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Service) deleteBlob(w http.ResponseWriter, r *http.Request, account, container, name string) {
	key := blobKey(account, container, name)
	found, err := s.db.Get(blobsBucket, key, &storedBlob{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "BlobNotFound",
			fmt.Sprintf("el blob '%s' no existe en el container '%s'", name, container))
		return
	}
	if err := s.db.Delete(blobsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func etagNow() string {
	return fmt.Sprintf("%q", time.Now().UTC().Format(time.RFC3339Nano))
}
