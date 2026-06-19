package cosmosdb

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const documentsBucket = "cosmosdb.documents"

// ServeHTTP atiende una request de data-plane de Cosmos DB ya enrutada
// por el dispatcher compartido (ver cmd/azure-emulator/main.go).
//
// Shape de URLs soportado (simplificado: PUT en vez de
// POST+x-ms-documentdb-partitionkey para crear, ver el comentario de
// accounts.go):
//
//	PUT    /{account}.documents/dbs/{db}/colls/{container}/docs/{id}  → crear o reemplazar un documento
//	GET    /{account}.documents/dbs/{db}/colls/{container}/docs/{id}  → leer un documento
//	GET    /{account}.documents/dbs/{db}/colls/{container}/docs       → listar documentos del container
//	DELETE /{account}.documents/dbs/{db}/colls/{container}/docs/{id}  → borrar un documento
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountResource := r.PathValue("accountResource")
	account, ok := strings.CutSuffix(accountResource, ".documents")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{account}.documents/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	parts := strings.Split(rest, "/")
	// parts esperado: ["dbs", "{db}", "colls", "{container}", "docs", ("{id}")]
	if len(parts) < 5 || parts[0] != "dbs" || parts[2] != "colls" || parts[4] != "docs" {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta de data plane inválida: se esperaba 'dbs/{db}/colls/{container}/docs[/{id}]'")
		return
	}
	dbName := parts[1]
	containerName := parts[3]

	exists, err := s.dataPlaneContainerExists(containerEntityPath(account, dbName, containerName))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("no existe el container '%s' en la base de datos '%s' de la cuenta '%s'", containerName, dbName, account))
		return
	}

	prefix := account + "/" + dbName + "/" + containerName + "/"

	switch {
	case len(parts) == 5:
		switch r.Method {
		case http.MethodGet:
			s.listDocuments(w, prefix)
		case http.MethodPost:
			s.createDocument(w, r, prefix)
		default:
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "método no soportado en 'docs'")
		}
	case len(parts) == 6:
		docID := parts[5]
		switch r.Method {
		case http.MethodPut:
			s.putDocument(w, r, prefix, docID)
		case http.MethodGet:
			s.getDocument(w, prefix, docID)
		case http.MethodDelete:
			s.deleteDocument(w, prefix, docID)
		default:
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "método no soportado en un documento")
		}
	default:
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", "ruta de data plane inválida bajo 'docs'")
	}
}

// putDocument crea o reemplaza el documento con id=docID. A diferencia
// de Azure real (donde el id va dentro del body JSON y la partitionKey
// se manda en un header), aquí se simplifica: el id va en la URL y el
// body completo (JSON arbitrario del cliente) se guarda tal cual.
func (s *Service) putDocument(w http.ResponseWriter, r *http.Request, prefix, docID string) {
	var doc map[string]any
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	doc["id"] = docID

	key := prefix + docID
	_, existedBefore, err := s.getDocumentRecord(key)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Put(documentsBucket, key, doc); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, doc)
}

// createDocument atiende POST .../docs sin id explícito en la URL,
// generando uno si el body no trae "id" — más cercano al POST real de
// Cosmos DB, ofrecido como alternativa a putDocument (PUT con id en la
// URL) para clientes que prefieran ese estilo.
func (s *Service) createDocument(w http.ResponseWriter, r *http.Request, prefix string) {
	var doc map[string]any
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	docID, _ := doc["id"].(string)
	if docID == "" {
		id, err := newDocID()
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		docID = id
		doc["id"] = docID
	}

	key := prefix + docID
	if err := s.db.Put(documentsBucket, key, doc); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, doc)
}

func (s *Service) getDocument(w http.ResponseWriter, prefix, docID string) {
	doc, found, err := s.getDocumentRecord(prefix + docID)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "NotFound",
			fmt.Sprintf("el documento '%s' no existe", docID))
		return
	}
	server.WriteJSON(w, http.StatusOK, doc)
}

func (s *Service) listDocuments(w http.ResponseWriter, prefix string) {
	docs := make([]map[string]any, 0)
	err := s.db.List(documentsBucket, prefix, func(key string, raw []byte) error {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"Documents": docs, "_count": len(docs)})
}

func (s *Service) deleteDocument(w http.ResponseWriter, prefix, docID string) {
	key := prefix + docID
	_, found, err := s.getDocumentRecord(key)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "NotFound",
			fmt.Sprintf("el documento '%s' no existe", docID))
		return
	}
	if err := s.db.Delete(documentsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) getDocumentRecord(key string) (map[string]any, bool, error) {
	var doc map[string]any
	found, err := s.db.Get(documentsBucket, key, &doc)
	return doc, found, err
}

// newDocID genera un identificador aleatorio (16 bytes en hex) para usar
// como id de documento cuando el cliente no especifica uno.
func newDocID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cosmosdb: error generando id aleatorio: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
