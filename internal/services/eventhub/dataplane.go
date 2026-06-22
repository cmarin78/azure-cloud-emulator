package eventhub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const eventsBucket = "eventhub.events"

// storedEvent es la forma persistida de un evento enviado a un event hub.
// Offset es un entero creciente por event hub (no por partición real: el
// data-plane simplificado de este emulador no reparte eventos entre
// particiones, ver eventhub.go) que el cliente usa para pedir "todo lo que
// venga después de N" -- el consumidor administra su propio checkpoint, el
// emulador no almacena progreso de consumo (a diferencia del peek-lock de
// Service Bus, Event Hubs real tampoco lo tiene: es un modelo de log, no
// de cola con ack).
type storedEvent struct {
	Offset       int64  `json:"offset"`
	Body         string `json:"body"`
	EnqueuedTime string `json:"enqueuedTimeUtc"`
}

// ServeHTTP atiende una request de data-plane de Event Hubs ya enrutada
// por el dispatcher compartido (ver cmd/azure-emulator/main.go).
//
// Shape de URLs soportado (simplificado -- ver eventhub.go):
//
//	POST /{namespace}.eventhub/{eventHubName}/messages                              → enviar un evento
//	GET  /{namespace}.eventhub/{eventHubName}/messages?offset=N&maxevents=M          → leer eventos con offset > N
//	GET  /{namespace}.eventhub/{eventHubName}/consumergroups/{cg}/messages?offset=N  → idéntico, validando que el consumer group exista
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountNS := r.PathValue("accountResource")
	namespace, ok := strings.CutSuffix(accountNS, ".eventhub")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{namespace}.eventhub/...'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta de data plane vacía: se esperaba '{eventHubName}/messages[...]'")
		return
	}
	hubName := parts[0]
	remainder := parts[1:]

	exists, err := s.dataPlaneHubExists(hubEntityPath(namespace, hubName))
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("no existe ningún event hub '%s' en el namespace '%s'", hubName, namespace))
		return
	}

	// "{eventHubName}/consumergroups/{cg}/messages" → solo valida que el
	// consumer group exista; la lectura en sí es idéntica a leer
	// directamente del event hub, porque el modelo simplificado no separa
	// el estado de lectura por consumer group.
	if len(remainder) >= 2 && remainder[0] == "consumergroups" {
		// No se valida que el consumer group exista vía ARM: el data-plane
		// no tiene subID/rg para consultar consumerGroupsBucket, y el
		// modelo simplificado no separa estado de lectura por consumer
		// group de todas formas -- basta con aceptar cualquier nombre,
		// igual que Azure real acepta "$Default" sin que el cliente lo
		// haya creado explícitamente.
		if len(remainder) < 3 || remainder[2] != "messages" {
			server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
				"ruta inválida bajo el consumer group: se esperaba '.../messages'")
			return
		}
		s.receiveEvents(w, r, namespace, hubName)
		return
	}

	if len(remainder) >= 1 && remainder[0] == "messages" {
		switch r.Method {
		case http.MethodPost:
			s.sendEvent(w, r, namespace, hubName)
		case http.MethodGet:
			s.receiveEvents(w, r, namespace, hubName)
		default:
			server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "método no soportado")
		}
		return
	}

	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		"ruta inválida bajo el event hub: se esperaba '.../messages' o '.../consumergroups/{cg}/messages'")
}

func eventPrefix(namespace, hub string) string {
	return namespace + "/" + hub + "/"
}

// sendEvent encola un evento nuevo con el siguiente offset disponible. El
// offset se deriva contando los eventos ya almacenados (suficiente para
// un emulador de un solo proceso; no hace falta un contador atómico
// separado porque BoltDB serializa escrituras).
func (s *Service) sendEvent(w http.ResponseWriter, r *http.Request, namespace, hub string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}

	prefix := eventPrefix(namespace, hub)
	var maxOffset int64 = -1
	err = s.db.List(eventsBucket, prefix, func(_ string, raw []byte) error {
		var ev storedEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return err
		}
		if ev.Offset > maxOffset {
			maxOffset = ev.Offset
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	offset := maxOffset + 1
	ev := storedEvent{
		Offset:       offset,
		Body:         string(body),
		EnqueuedTime: time.Now().UTC().Format(time.RFC1123),
	}
	key := fmt.Sprintf("%s%020d", prefix, offset)
	if err := s.db.Put(eventsBucket, key, ev); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusCreated, ev)
}

// receiveEvents devuelve los eventos con offset estrictamente mayor que
// "?offset=" (por defecto -1, es decir, desde el principio), hasta
// "?maxevents=" (por defecto 32). El cliente es responsable de recordar
// el último offset leído y pedir desde ahí la próxima vez -- no hay
// checkpoint server-side por consumer group, a diferencia del
// almacenamiento de checkpoints en Blob Storage que usa el SDK real.
func (s *Service) receiveEvents(w http.ResponseWriter, r *http.Request, namespace, hub string) {
	afterOffset := int64(-1)
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			afterOffset = parsed
		}
	}
	maxEvents := 32
	if v := r.URL.Query().Get("maxevents"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			maxEvents = parsed
		}
	}

	prefix := eventPrefix(namespace, hub)
	var all []storedEvent
	err := s.db.List(eventsBucket, prefix, func(_ string, raw []byte) error {
		var ev storedEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return err
		}
		if ev.Offset > afterOffset {
			all = append(all, ev)
		}
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// db.List ya itera en orden de clave, y las claves usan offset
	// zero-padded, así que "all" ya viene ordenado por offset ascendente.
	if len(all) > maxEvents {
		all = all[:maxEvents]
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": all})
}
