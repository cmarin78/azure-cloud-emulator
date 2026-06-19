package servicebus

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
)

const messagesBucket = "servicebus.messages"

const defaultLockDuration = 60 * time.Second

// Message es la forma pública de un mensaje, devuelta por send/receive.
// LockToken va vacío en las respuestas de peek (igual que el data-plane de
// Storage Queue: peek no "bloquea" el mensaje, así que no hay token válido
// para completarlo).
type Message struct {
	MessageID     string `json:"messageId"`
	LockToken     string `json:"lockToken,omitempty"`
	Body          string `json:"body"`
	EnqueuedTime  string `json:"enqueuedTimeUtc"`
	DeliveryCount int    `json:"deliveryCount"`
	LockedUntil   string `json:"lockedUntilUtc,omitempty"`
}

// storedMessage añade a Message los campos internos para controlar el
// peek-lock (cuándo vuelve a ser elegible para Receive tras haber sido
// entregado a un consumidor) sin exponerlos más allá de lo que la API
// real expone.
type storedMessage struct {
	Message
	LockedUntil time.Time `json:"lockedUntil"`
}

// ServeHTTP atiende una request de data-plane de Service Bus ya enrutada
// por el dispatcher compartido (ver cmd/azure-emulator/main.go y el
// comentario de queue.Service.ServeHTTP para el porqué de un dispatcher
// único en vez de un mux.HandleFunc por servicio).
//
// Shape de URLs soportado:
//
//	POST /{namespace}.servicebus/{queue}/messages                                    → enviar a una cola
//	GET  /{namespace}.servicebus/{queue}/messages?peeklock=true                       → recibir (peek-lock) de una cola
//	DELETE /{namespace}.servicebus/{queue}/messages/{messageId}?lockToken=...         → completar (borrar) un mensaje de cola
//	POST /{namespace}.servicebus/{topic}/messages                                     → enviar a un topic (fan-out a sus subscriptions)
//	GET  /{namespace}.servicebus/{topic}/subscriptions/{sub}/messages?peeklock=true    → recibir de una subscription
//	DELETE /{namespace}.servicebus/{topic}/subscriptions/{sub}/messages/{messageId}?lockToken=...  → completar un mensaje de subscription
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountNS := r.PathValue("accountResource")
	namespace, ok := strings.CutSuffix(accountNS, ".servicebus")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{namespace}.servicebus/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta de data plane vacía: se esperaba '{queue|topic}/messages[...]'")
		return
	}
	entity := parts[0]
	remainder := parts[1:]

	// "{entity}/subscriptions/{sub}/messages[...]" → topic + subscription.
	if len(remainder) >= 2 && remainder[0] == "subscriptions" {
		subName := remainder[1]
		subRest := remainder[2:]
		s.handleSubscriptionMessages(w, r, namespace, entity, subName, subRest)
		return
	}

	// "{entity}/messages[...]" → cola o topic (envío únicamente).
	if len(remainder) >= 1 && remainder[0] == "messages" {
		s.handleEntityMessages(w, r, namespace, entity, remainder[1:])
		return
	}

	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		"ruta inválida bajo el namespace: se esperaba '.../messages', '.../messages/{id}' o '.../subscriptions/{sub}/messages[...]'")
}

// handleEntityMessages atiende el envío a una cola o a un topic (POST) y
// la recepción/completado para una cola (colas no tienen subscriptions
// intermedias, así que reciben directamente de su propia cola de
// mensajes).
func (s *Service) handleEntityMessages(w http.ResponseWriter, r *http.Request, namespace, entity string, rest []string) {
	isQueue, err := s.dataPlaneEntityExists(queueEntityPath(namespace, entity))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	isTopic := false
	if !isQueue {
		isTopic, err = s.dataPlaneEntityExists(topicEntityPath(namespace, entity))
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}
	if !isQueue && !isTopic {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("no existe ninguna cola ni topic '%s' en el namespace '%s'", entity, namespace))
		return
	}

	switch {
	case len(rest) == 0:
		switch r.Method {
		case http.MethodPost:
			if isTopic {
				s.sendToTopic(w, r, namespace, entity)
			} else {
				s.sendMessage(w, r, messagePrefixFor(namespace, entity))
			}
		case http.MethodGet:
			if isTopic {
				server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
					"no se puede recibir directamente de un topic: usar una subscription")
				return
			}
			s.receiveMessages(w, r, messagePrefixFor(namespace, entity))
		default:
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "método no soportado")
		}
	case len(rest) == 1:
		if r.Method != http.MethodDelete {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"método no soportado para un mensaje individual")
			return
		}
		if isTopic {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"no se puede completar un mensaje directamente en un topic: usar una subscription")
			return
		}
		s.completeMessage(w, r, messagePrefixFor(namespace, entity), rest[0])
	default:
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", "ruta inválida bajo messages")
	}
}

func (s *Service) handleSubscriptionMessages(w http.ResponseWriter, r *http.Request, namespace, topic, subName string, rest []string) {
	exists, err := s.dataPlaneEntityExists(subscriptionEntityPath(namespace, topic, subName))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("no existe la subscription '%s' en el topic '%s' del namespace '%s'", subName, topic, namespace))
		return
	}
	if len(rest) == 0 || rest[0] != "messages" {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta inválida bajo la subscription: se esperaba '.../messages' o '.../messages/{id}'")
		return
	}
	prefix := subscriptionMessagePrefix(namespace, topic, subName)

	switch {
	case len(rest) == 1:
		if r.Method != http.MethodGet {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"a nivel de subscription solo se soporta GET (receive); para enviar, usar el topic")
			return
		}
		s.receiveMessages(w, r, prefix)
	case len(rest) == 2:
		if r.Method != http.MethodDelete {
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
				"método no soportado para un mensaje individual")
			return
		}
		s.completeMessage(w, r, prefix, rest[1])
	default:
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", "ruta inválida bajo messages")
	}
}

func messagePrefixFor(namespace, entity string) string {
	return namespace + "/" + entity + "/"
}

func subscriptionMessagePrefix(namespace, topic, sub string) string {
	return namespace + "/" + topic + "/subscriptions/" + sub + "/"
}

// sendToTopic hace fan-out: el mismo mensaje se copia a la cola de
// mensajes de cada subscription activa del topic, igual que Service Bus
// real entrega una copia independiente a cada subscription.
func (s *Service) sendToTopic(w http.ResponseWriter, r *http.Request, namespace, topic string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}

	subPrefix := namespace + "/" + topic + "/"
	var subNames []string
	err = s.db.List(subscriptionsBucket, "", func(key string, raw []byte) error {
		var sub Subscription
		if err := json.Unmarshal(raw, &sub); err != nil {
			return err
		}
		if strings.Contains(sub.ID, "/topics/"+topic+"/subscriptions/") {
			subNames = append(subNames, sub.Name)
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	sent := make([]Message, 0, len(subNames))
	for _, subName := range subNames {
		msg, err := s.storeMessage(subPrefix+"subscriptions/"+subName+"/", body)
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		sent = append(sent, msg)
	}
	server.WriteJSON(w, http.StatusCreated, map[string]any{"deliveredTo": len(subNames), "value": sent})
}

// sendMessage encola un mensaje nuevo para una cola. Igual que
// queue.putMessage, se acepta el body crudo de la request como texto del
// mensaje (sin el XML/AMQP real).
func (s *Service) sendMessage(w http.ResponseWriter, r *http.Request, prefix string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}
	msg, err := s.storeMessage(prefix, body)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, map[string]any{"value": []Message{msg}})
}

func (s *Service) storeMessage(prefix string, body []byte) (Message, error) {
	id, err := newID()
	if err != nil {
		return Message{}, err
	}
	now := time.Now().UTC()
	rec := storedMessage{
		Message: Message{
			MessageID:     id,
			Body:          string(body),
			EnqueuedTime:  now.Format(time.RFC1123),
			DeliveryCount: 0,
		},
		LockedUntil: now, // disponible de inmediato hasta que un Receive la bloquee
	}
	if err := s.db.Put(messagesBucket, prefix+id, rec); err != nil {
		return Message{}, err
	}
	return rec.Message, nil
}

// receiveMessages atiende tanto "Receive" en modo peek-lock (bloquea los
// mensajes devueltos por lockDuration y aumenta su deliveryCount) como
// "Peek" (?peeklock=false: solo lee, no bloquea ni cambia deliveryCount,
// y no expone lockToken) — mismo patrón que queue.getMessages.
func (s *Service) receiveMessages(w http.ResponseWriter, r *http.Request, prefix string) {
	n := 1
	if v := r.URL.Query().Get("maxmessages"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 32 {
		n = 32
	}
	peekOnly := r.URL.Query().Get("peeklock") == "false"

	lockDuration := defaultLockDuration
	if v := r.URL.Query().Get("locktimeout"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			lockDuration = time.Duration(secs) * time.Second
		}
	}

	now := time.Now().UTC()
	var candidates []storedMessage
	err := s.db.List(messagesBucket, prefix, func(_ string, raw []byte) error {
		var rec storedMessage
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		if !rec.LockedUntil.After(now) {
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
			msg.LockToken = ""
			msg.LockedUntil = ""
			result = append(result, msg)
			continue
		}

		token, err := newID()
		if err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		rec.LockToken = token
		rec.DeliveryCount++
		rec.LockedUntil = now.Add(lockDuration)
		rec.Message.LockedUntil = rec.LockedUntil.Format(time.RFC1123)
		if err := s.db.Put(messagesBucket, prefix+rec.MessageID, rec); err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		result = append(result, rec.Message)
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": result})
}

// completeMessage borra un mensaje ya recibido (DELETE
// .../messages/{id}?lockToken=...). Azure real exige que el lockToken
// coincida con el de la última recepción, para evitar que un consumidor
// complete un mensaje que ya fue re-entregado a otro tras expirar su
// lock — mismo patrón que queue.handleMessage con popReceipt.
func (s *Service) completeMessage(w http.ResponseWriter, r *http.Request, prefix, messageID string) {
	lockToken := r.URL.Query().Get("lockToken")
	if lockToken == "" {
		server.WriteError(w, http.StatusBadRequest, "InvalidQueryParameterValue",
			"se esperaba '?lockToken=' para completar un mensaje")
		return
	}

	key := prefix + messageID
	var rec storedMessage
	found, err := s.db.Get(messagesBucket, key, &rec)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "MessageNotFound",
			fmt.Sprintf("el mensaje '%s' no existe", messageID))
		return
	}
	if rec.LockToken != lockToken {
		server.WriteError(w, http.StatusBadRequest, "LockTokenMismatch",
			"el lockToken no coincide con la última recepción del mensaje")
		return
	}
	if err := s.db.Delete(messagesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newID genera un identificador aleatorio (16 bytes en hex) para usar
// como messageId o lockToken. No hace falta una librería de UUID: solo se
// necesita unicidad práctica, no el formato canónico.
func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("servicebus: error generando id aleatorio: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
