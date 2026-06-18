// Package queue emula el data plane de Azure Queue Storage: colas y
// mensajes dentro de una storage account.
//
// Sigue exactamente el mismo patrón que internal/services/blob (ver el
// comentario de ese paquete para el razonamiento completo): el data plane
// real de Azure vive en https://{account}.queue.core.windows.net/{queue}/...,
// y como este emulador no tiene un host por cuenta, storageaccounts.go ya
// devuelve endpoints "path-style" (http://{emulador}/{account}.queue/...) —
// este paquete sirve ese shape, con "{account}.queue" como primer segmento
// del path en vez de como host.
//
// Las mutaciones son síncronas (New(db), sin LRO/ops): crear una cola o
// encolar/desencolar un mensaje no es una operación de larga duración en
// Azure real. Igual que blob.go, las respuestas usan JSON en vez del XML
// real de Queue Storage — el emulador no implementa autenticación
// AAD/SharedKey todavía, así que SDKs/CLIs reales no pueden apuntar aquí
// sin parches; JSON simple basta para los smoke tests de este proyecto.
//
// Shape de URLs soportado (mismo que la API real, menos XML):
//
//	GET    /{account}.queue/?comp=list                          → listar colas
//	PUT    /{account}.queue/{queue}                              → crear cola
//	GET    /{account}.queue/{queue}?comp=metadata                → metadata de la cola
//	DELETE /{account}.queue/{queue}                              → borrar cola (+ sus mensajes)
//	POST   /{account}.queue/{queue}/messages                     → encolar mensaje
//	GET    /{account}.queue/{queue}/messages?numofmessages=&visibilitytimeout=  → desencolar (peek con ?peekonly=true)
//	DELETE /{account}.queue/{queue}/messages                     → vaciar la cola
//	DELETE /{account}.queue/{queue}/messages/{messageId}?popreceipt=...  → borrar un mensaje puntual
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const queuesBucket = "queue.queues"
const messagesBucket = "queue.messages"

const defaultVisibilityTimeout = 30 * time.Second

// Service agrupa el estado necesario para atender las rutas de data plane
// de Queue Storage.
type Service struct {
	db *storage.DB
}

// New crea el servicio de colas/mensajes.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Queue replica el subconjunto relevante de metadata de una cola.
type Queue struct {
	Name         string `json:"name"`
	Account      string `json:"accountName"`
	LastModified string `json:"lastModified"`
}

// Message es la forma pública de un mensaje, devuelta por
// put/get/peek-messages. PopReceipt va vacío en las respuestas de peek
// (igual que Azure real: peek no "reserva" el mensaje, así que no hay
// receipt válido para borrarlo).
type Message struct {
	ID              string `json:"messageId"`
	PopReceipt      string `json:"popReceipt,omitempty"`
	Queue           string `json:"queue"`
	MessageText     string `json:"messageText"`
	InsertionTime   string `json:"insertionTime"`
	ExpirationTime  string `json:"expirationTime"`
	DequeueCount    int    `json:"dequeueCount"`
	NextVisibleTime string `json:"nextVisibleTime,omitempty"`
}

// storedMessage añade a Message los campos internos necesarios para
// controlar visibilidad (cuándo vuelve a ser elegible para Get Messages
// tras haber sido leído) sin exponerlos directamente como timestamps
// parseables por el cliente más allá de lo que la API real expone.
type storedMessage struct {
	Message
	Account     string    `json:"account"`
	NextVisible time.Time `json:"nextVisible"`
}

// ServeHTTP atiende una request de data plane de colas ya enrutada por el
// dispatcher compartido (ver cmd/azure-emulator/main.go y el comentario de
// blob.Service.ServeHTTP: blob y queue no pueden registrar cada uno su
// propio "/{accountX}/{path...}" en el mux porque net/http.ServeMux trata
// ambos patrones como la misma forma de ruta sin importar el nombre del
// wildcard, así que un único dispatcher central registra el patrón una
// vez y despacha por sufijo ".blob"/".queue" del primer segmento).
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountQueue := r.PathValue("accountResource")
	account, ok := strings.CutSuffix(accountQueue, ".queue")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{account}.queue/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	var queue, sub string
	if rest != "" {
		parts := strings.SplitN(rest, "/", 2)
		queue = parts[0]
		if len(parts) == 2 {
			sub = parts[1]
		}
	}

	switch {
	case queue == "":
		s.handleAccount(w, r, account)
	case sub == "":
		s.handleQueue(w, r, account, queue)
	case sub == "messages":
		s.handleMessages(w, r, account, queue)
	default:
		// sub tiene la forma "messages/{messageId}".
		messageID, ok := strings.CutPrefix(sub, "messages/")
		if !ok {
			server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
				"ruta inválida bajo la cola: se esperaba '.../messages' o '.../messages/{id}'")
			return
		}
		s.handleMessage(w, r, account, queue, messageID)
	}
}

func queueKey(account, queue string) string {
	return account + "/" + queue
}

func messagePrefix(account, queue string) string {
	return account + "/" + queue + "/"
}

func messageKey(account, queue, id string) string {
	return account + "/" + queue + "/" + id
}

// handleAccount atiende operaciones a nivel de cuenta: hoy solo
// "List Queues" (GET /{account}.queue/?comp=list).
func (s *Service) handleAccount(w http.ResponseWriter, r *http.Request, account string) {
	if r.Method != http.MethodGet {
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"a nivel de cuenta solo se soporta GET (list queues)")
		return
	}
	if r.URL.Query().Get("comp") != "list" {
		server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"se esperaba '?comp=list' para listar colas de la cuenta")
		return
	}

	queues := make([]Queue, 0)
	err := s.db.List(queuesBucket, account+"/", func(key string, raw []byte) error {
		var q Queue
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		queues = append(queues, q)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": queues})
}

// handleQueue despacha las operaciones sobre una cola según el método
// HTTP: crear (PUT), metadata (GET ?comp=metadata) y borrar (DELETE).
func (s *Service) handleQueue(w http.ResponseWriter, r *http.Request, account, queue string) {
	switch r.Method {
	case http.MethodPut:
		s.createQueue(w, r, account, queue)
	case http.MethodGet:
		if r.URL.Query().Get("comp") != "metadata" {
			server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
				"se esperaba '?comp=metadata' para obtener metadata de la cola")
			return
		}
		s.getQueue(w, r, account, queue)
	case http.MethodDelete:
		s.deleteQueue(w, r, account, queue)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para colas")
	}
}

func (s *Service) createQueue(w http.ResponseWriter, r *http.Request, account, queue string) {
	key := queueKey(account, queue)
	var existing Queue
	found, err := s.db.Get(queuesBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if found {
		server.WriteError(w, http.StatusConflict, "QueueAlreadyExists",
			fmt.Sprintf("la cola '%s' ya existe en la cuenta '%s'", queue, account))
		return
	}

	q := Queue{
		Name:         queue,
		Account:      account,
		LastModified: time.Now().UTC().Format(time.RFC1123),
	}
	if err := s.db.Put(queuesBucket, key, q); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("Last-Modified", q.LastModified)
	w.WriteHeader(http.StatusCreated)
}

func (s *Service) getQueue(w http.ResponseWriter, r *http.Request, account, queue string) {
	var q Queue
	found, err := s.db.Get(queuesBucket, queueKey(account, queue), &q)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "QueueNotFound",
			fmt.Sprintf("la cola '%s' no existe en la cuenta '%s'", queue, account))
		return
	}

	count := 0
	err = s.db.List(messagesBucket, messagePrefix(account, queue), func(string, []byte) error {
		count++
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.Header().Set("x-ms-approximate-messages-count", strconv.Itoa(count))
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"name":                     q.Name,
		"accountName":              q.Account,
		"lastModified":             q.LastModified,
		"approximateMessagesCount": count,
	})
}

// deleteQueue también borra en cascada todos los mensajes pendientes,
// igual que deleteContainer en blob.go.
func (s *Service) deleteQueue(w http.ResponseWriter, r *http.Request, account, queue string) {
	key := queueKey(account, queue)
	found, err := s.db.Get(queuesBucket, key, &Queue{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "QueueNotFound",
			fmt.Sprintf("la cola '%s' no existe en la cuenta '%s'", queue, account))
		return
	}

	if err := s.clearMessagesInternal(account, queue); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Delete(queuesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMessages despacha las operaciones sobre la colección de mensajes
// de una cola: encolar (POST), obtener/peek (GET) y vaciar (DELETE).
func (s *Service) handleMessages(w http.ResponseWriter, r *http.Request, account, queue string) {
	if !s.queueExists(w, account, queue) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.putMessage(w, r, account, queue)
	case http.MethodGet:
		s.getMessages(w, r, account, queue)
	case http.MethodDelete:
		s.clearMessages(w, r, account, queue)
	default:
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para la colección de mensajes")
	}
}

// queueExists escribe la respuesta de error 404 y devuelve false si la
// cola no existe; los handlers de mensajes llaman esto primero para no
// repetir la misma comprobación en cada operación.
func (s *Service) queueExists(w http.ResponseWriter, account, queue string) bool {
	found, err := s.db.Get(queuesBucket, queueKey(account, queue), &Queue{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return false
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "QueueNotFound",
			fmt.Sprintf("la cola '%s' no existe en la cuenta '%s'", queue, account))
		return false
	}
	return true
}

// putMessage encola un mensaje nuevo. Azure real espera un cuerpo XML
// (<QueueMessage><MessageText>...</MessageText></QueueMessage>); aquí,
// igual que blob.putBlob, se acepta el texto del mensaje directamente
// como cuerpo crudo de la request (texto plano), siguiendo la
// simplificación JSON/texto-plano ya establecida para este emulador.
func (s *Service) putMessage(w http.ResponseWriter, r *http.Request, account, queue string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}

	id, err := newID()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	popReceipt, err := newID()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	now := time.Now().UTC()
	ttl := 7 * 24 * time.Hour // mismo default que Azure real (7 días)
	if v := r.URL.Query().Get("messagettl"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}

	rec := storedMessage{
		Message: Message{
			ID:             id,
			PopReceipt:     popReceipt,
			Queue:          queue,
			MessageText:    string(body),
			InsertionTime:  now.Format(time.RFC1123),
			ExpirationTime: now.Add(ttl).Format(time.RFC1123),
			DequeueCount:   0,
		},
		Account:     account,
		NextVisible: now, // visible de inmediato hasta que un Get la "reserve"
	}
	if err := s.db.Put(messagesBucket, messageKey(account, queue, id), rec); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, map[string]any{"value": []Message{rec.Message}})
}

// getMessages atiende tanto "Get Messages" (desencola: reserva los
// mensajes devueltos ocultándolos por visibilitytimeout y aumenta su
// dequeueCount) como "Peek Messages" (?peekonly=true: solo lee, no
// reserva ni cambia dequeueCount, y no expone popReceipt).
func (s *Service) getMessages(w http.ResponseWriter, r *http.Request, account, queue string) {
	n := 1
	if v := r.URL.Query().Get("numofmessages"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 32 {
		n = 32 // mismo tope que la API real
	}
	peekOnly := r.URL.Query().Get("peekonly") == "true"

	visibility := defaultVisibilityTimeout
	if v := r.URL.Query().Get("visibilitytimeout"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			visibility = time.Duration(secs) * time.Second
		}
	}

	now := time.Now().UTC()
	var candidates []storedMessage
	err := s.db.List(messagesBucket, messagePrefix(account, queue), func(_ string, raw []byte) error {
		var rec storedMessage
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		if !rec.NextVisible.After(now) {
			candidates = append(candidates, rec)
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	result := make([]Message, 0, n)
	for i := 0; i < len(candidates) && len(result) < n; i++ {
		rec := candidates[i]
		if peekOnly {
			msg := rec.Message
			msg.PopReceipt = ""
			msg.NextVisibleTime = ""
			result = append(result, msg)
			continue
		}

		newPopReceipt, err := newID()
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		rec.PopReceipt = newPopReceipt
		rec.DequeueCount++
		rec.NextVisible = now.Add(visibility)
		rec.Message.NextVisibleTime = rec.NextVisible.Format(time.RFC1123)
		if err := s.db.Put(messagesBucket, messageKey(account, queue, rec.ID), rec); err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		result = append(result, rec.Message)
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": result})
}

func (s *Service) clearMessages(w http.ResponseWriter, r *http.Request, account, queue string) {
	if err := s.clearMessagesInternal(account, queue); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) clearMessagesInternal(account, queue string) error {
	var keysToDelete []string
	err := s.db.List(messagesBucket, messagePrefix(account, queue), func(k string, _ []byte) error {
		keysToDelete = append(keysToDelete, k)
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range keysToDelete {
		if err := s.db.Delete(messagesBucket, k); err != nil {
			return err
		}
	}
	return nil
}

// handleMessage atiende el borrado de un mensaje puntual tras leerlo
// (DELETE .../messages/{id}?popreceipt=...). Azure real exige que el
// popreceipt coincida con el de la última lectura, para evitar que un
// consumidor borre un mensaje que ya fue re-entregado a otro consumidor
// tras expirar su visibilidad.
func (s *Service) handleMessage(w http.ResponseWriter, r *http.Request, account, queue, messageID string) {
	if r.Method != http.MethodDelete {
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
			"método no soportado para un mensaje individual")
		return
	}
	if !s.queueExists(w, account, queue) {
		return
	}

	popReceipt := r.URL.Query().Get("popreceipt")
	if popReceipt == "" {
		server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"se esperaba '?popreceipt=' para borrar un mensaje")
		return
	}

	key := messageKey(account, queue, messageID)
	var rec storedMessage
	found, err := s.db.Get(messagesBucket, key, &rec)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "MessageNotFound",
			fmt.Sprintf("el mensaje '%s' no existe en la cola '%s'", messageID, queue))
		return
	}
	if rec.PopReceipt != popReceipt {
		server.WriteError(w, http.StatusBadRequest, "PopReceiptMismatch",
			"el popreceipt no coincide con la última lectura del mensaje")
		return
	}
	if err := s.db.Delete(messagesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newID genera un identificador aleatorio (16 bytes en hex) para usar
// como messageId o popReceipt. No hace falta una librería de UUID: solo
// se necesita unicidad práctica, no el formato canónico.
func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("queue: error generando id aleatorio: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
