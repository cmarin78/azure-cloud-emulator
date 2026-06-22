package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const actionGroupsBucket = "monitor.actionGroups"

// ActionGroup replica el subconjunto relevante de Microsoft.Insights/
// actionGroups. Los receptores email/sms/azureFunction se mantienen como
// json.RawMessage porque su estructura interna no es relevante para el
// emulador -- nunca se envía nada por esos canales. webhookReceivers, en
// cambio, sí se despacha de verdad (Fase 20: capa de comportamiento) --
// mismo enfoque que gcp-emulator usa para el push real de Pub/Sub
// (internal/services/pubsub) y el despacho real de Cloud Scheduler/Cloud
// Tasks: un *http.Client con timeout corto, sin reintentos ni
// dead-lettering, documentado como limitación conocida.
type ActionGroup struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Location   string                `json:"location"`
	Tags       map[string]string     `json:"tags,omitempty"`
	Properties ActionGroupProperties `json:"properties"`
}

type ActionGroupProperties struct {
	GroupShortName         string          `json:"groupShortName"`
	Enabled                bool            `json:"enabled"`
	EmailReceivers         json.RawMessage `json:"emailReceivers,omitempty"`
	SmsReceivers           json.RawMessage `json:"smsReceivers,omitempty"`
	WebhookReceivers       json.RawMessage `json:"webhookReceivers,omitempty"`
	AzureFunctionReceivers json.RawMessage `json:"azureFunctionReceivers,omitempty"`

	// LastNotificationTime/LastNotificationStatus son una extensión del
	// emulador (no existen en el shape real de Azure) para poder observar
	// el resultado del despacho real de webhooks sin necesitar un sink de
	// activity log propio todavía -- ver nota de Fase 20 en ROADMAP.md. Los
	// clientes reales (az CLI/Terraform) ignoran campos desconocidos en la
	// respuesta JSON, así que esto no rompe compatibilidad.
	LastNotificationTime   string `json:"lastNotificationTime,omitempty"`
	LastNotificationStatus string `json:"lastNotificationStatus,omitempty"`
}

// webhookReceiver replica el subconjunto relevante de Microsoft.Insights/
// actionGroups#WebhookReceiver necesario para despachar de verdad.
type webhookReceiver struct {
	Name                 string `json:"name"`
	ServiceURI           string `json:"serviceUri"`
	UseCommonAlertSchema bool   `json:"useCommonAlertSchema,omitempty"`
}

// testNotificationPayload imita el "common alert schema" real de Azure
// Monitor lo suficiente para que un receptor de webhook pueda distinguir un
// disparo de prueba; no pretende ser una réplica completa del schema real.
type testNotificationPayload struct {
	SchemaID string `json:"schemaId"`
	Data     struct {
		Essentials struct {
			AlertID          string `json:"alertId"`
			AlertRule        string `json:"alertRule"`
			Severity         string `json:"severity"`
			SignalType       string `json:"signalType"`
			MonitorCondition string `json:"monitorCondition"`
			FiredDateTime    string `json:"firedDateTime"`
		} `json:"essentials"`
	} `json:"data"`
}

// dispatch envía un POST HTTP real a cada webhookReceiver del action group y
// registra el resultado (LastNotificationTime/LastNotificationStatus) sobre
// el registro persistido. Corre síncrono (no en su propia goroutine) porque
// el único punto de disparo hoy es la acción explícita createNotifications,
// no un pipeline de métricas continuo -- a diferencia de Cloud Scheduler en
// gcp-emulator, no hace falta un goroutine de larga duración.
func (s *Service) dispatch(subID, rg, name string) (status string, dispatched int) {
	ag, found, err := s.getActionGroup(subID, rg, name)
	if err != nil || !found {
		return "action group not found", 0
	}

	var receivers []webhookReceiver
	if len(ag.Properties.WebhookReceivers) > 0 {
		_ = json.Unmarshal(ag.Properties.WebhookReceivers, &receivers)
	}

	payload := testNotificationPayload{SchemaID: "azureMonitorCommonAlertSchema"}
	payload.Data.Essentials.AlertID = actionGroupID(subID, rg, name)
	payload.Data.Essentials.AlertRule = "test notification"
	payload.Data.Essentials.Severity = "Sev4"
	payload.Data.Essentials.SignalType = "Metric"
	payload.Data.Essentials.MonitorCondition = "Fired"
	payload.Data.Essentials.FiredDateTime = time.Now().UTC().Format(time.RFC3339)
	body, _ := json.Marshal(payload)

	overallStatus := "ok"
	switch {
	case !ag.Properties.Enabled:
		overallStatus = "skipped: action group disabled"
	case len(receivers) == 0:
		overallStatus = "skipped: no webhook receivers configured"
	default:
		for _, recv := range receivers {
			if strings.TrimSpace(recv.ServiceURI) == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, recv.ServiceURI, bytes.NewReader(body))
			if err != nil {
				overallStatus = err.Error()
				cancel()
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := s.httpClient.Do(req)
			cancel()
			if err != nil {
				overallStatus = err.Error()
				continue
			}
			resp.Body.Close()
			dispatched++
			if resp.StatusCode >= 400 {
				overallStatus = fmt.Sprintf("http %d from %s", resp.StatusCode, recv.Name)
			}
		}
	}

	ag.Properties.LastNotificationTime = time.Now().UTC().Format(time.RFC3339)
	ag.Properties.LastNotificationStatus = overallStatus
	_ = s.db.Put(actionGroupsBucket, actionGroupKey(subID, rg, name), ag)
	return overallStatus, dispatched
}

type actionGroupRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		GroupShortName         string          `json:"groupShortName"`
		Enabled                *bool           `json:"enabled"`
		EmailReceivers         json.RawMessage `json:"emailReceivers"`
		SmsReceivers           json.RawMessage `json:"smsReceivers"`
		WebhookReceivers       json.RawMessage `json:"webhookReceivers"`
		AzureFunctionReceivers json.RawMessage `json:"azureFunctionReceivers"`
	} `json:"properties"`
}

func actionGroupKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func actionGroupID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/actionGroups/%s", subID, rg, name)
}

func (s *Service) registerActionGroups(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Insights/actionGroups"
	mux.HandleFunc("GET "+base, s.listActionGroups)
	mux.HandleFunc("PUT "+base+"/{actionGroupName}", s.putActionGroup)
	mux.HandleFunc("GET "+base+"/{actionGroupName}", s.getActionGroupHandler)
	mux.HandleFunc("DELETE "+base+"/{actionGroupName}", s.deleteActionGroup)
	// createNotifications es el nombre real de la acción de Azure Monitor
	// para disparar una notificación de prueba (a diferencia del patrón
	// "{name}:action" de gcp-emulator, Azure usa un sub-path literal, igual
	// que start/stop/restart en appservice). Fase 20: ahora dispara un POST
	// HTTP real a cada webhookReceiver en vez de solo validar.
	mux.HandleFunc("POST "+base+"/{actionGroupName}/createNotifications", s.createNotifications)
}

// createNotifications dispara un envío real (Fase 20) a los webhookReceivers
// del action group. Síncrono: espera a que todos los despachos terminen
// antes de responder, ya que el timeout por receiver es corto (10s) y no
// hay necesidad de un flujo async/LRO para esto.
func (s *Service) createNotifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("actionGroupName")

	if _, found, err := s.getActionGroup(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el action group '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	status, dispatched := s.dispatch(subID, rg, name)
	server.WriteJSON(w, http.StatusOK, map[string]any{
		"correlationId":      fmt.Sprintf("%s-test", name),
		"dispatchedCount":    dispatched,
		"notificationStatus": status,
	})
}

func (s *Service) putActionGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("actionGroupName")

	var req actionGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio (azurerm_monitor_action_group usa 'global')")
		return
	}
	if strings.TrimSpace(req.Properties.GroupShortName) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.groupShortName' es obligatorio")
		return
	}

	enabled := true
	if req.Properties.Enabled != nil {
		enabled = *req.Properties.Enabled
	}

	key := actionGroupKey(subID, rg, name)
	_, found, err := s.getActionGroup(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	ag := ActionGroup{
		ID:       actionGroupID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Insights/actionGroups",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: ActionGroupProperties{
			GroupShortName:         req.Properties.GroupShortName,
			Enabled:                enabled,
			EmailReceivers:         req.Properties.EmailReceivers,
			SmsReceivers:           req.Properties.SmsReceivers,
			WebhookReceivers:       req.Properties.WebhookReceivers,
			AzureFunctionReceivers: req.Properties.AzureFunctionReceivers,
		},
	}

	if err := s.db.Put(actionGroupsBucket, key, ag); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, ag)
}

func (s *Service) getActionGroup(subID, rg, name string) (ActionGroup, bool, error) {
	var ag ActionGroup
	found, err := s.db.Get(actionGroupsBucket, actionGroupKey(subID, rg, name), &ag)
	return ag, found, err
}

func (s *Service) getActionGroupHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("actionGroupName")

	ag, found, err := s.getActionGroup(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el action group '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, ag)
}

func (s *Service) listActionGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	groups := make([]ActionGroup, 0)
	err := s.db.List(actionGroupsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var ag ActionGroup
		if err := json.Unmarshal(raw, &ag); err != nil {
			return err
		}
		groups = append(groups, ag)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": groups})
}

// deleteActionGroup es idempotente (204 si no existe), igual que deleteVault.
func (s *Service) deleteActionGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("actionGroupName")
	key := actionGroupKey(subID, rg, name)

	found, err := s.db.Get(actionGroupsBucket, key, &ActionGroup{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(actionGroupsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
