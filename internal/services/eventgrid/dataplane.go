package eventgrid

import (
	"io"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// ServeHTTP atiende una request de data-plane de Event Grid ya enrutada
// por el dispatcher compartido (ver cmd/azure-emulator/main.go).
//
// Shape de URL soportado:
//
//	POST /{topicName}.eventgrid/api/events  → publicar un batch de eventos
//
// Real Azure acepta tanto el schema de Event Grid como CloudEvents en este
// endpoint; aquí no se valida la forma de cada evento individual (el body
// se reenvía tal cual a cada webhook suscrito) -- "shape-compatible, no
// behavior-complete", igual que el resto del proyecto.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	accountResource := r.PathValue("accountResource")
	topicName, ok := strings.CutSuffix(accountResource, ".eventgrid")
	if !ok {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"endpoint de data plane inválido: se esperaba el shape '{topic}.eventgrid/api/events'")
		return
	}

	rest := strings.Trim(r.PathValue("path"), "/")
	if rest != "api/events" {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"ruta inválida bajo el topic: se esperaba 'api/events'")
		return
	}
	if r.Method != http.MethodPost {
		server.WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "solo se soporta POST para publicar eventos")
		return
	}

	ref, found, err := s.lookupDataPlaneTopic(topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			"el topic '"+topicName+"' no existe")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())
		return
	}

	subs, err := s.listEventSubscriptionRecords(ref.SubscriptionID, ref.ResourceGroup, topicName)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	for _, es := range subs {
		name := es.Name
		go s.dispatchToSubscription(ref.SubscriptionID, ref.ResourceGroup, topicName, name, body)
	}

	// Azure real responde 200 con un body vacío "{}" al publicar.
	server.WriteJSON(w, http.StatusOK, map[string]any{})
}
