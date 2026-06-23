// Package logicapps emula un subconjunto de Microsoft.Logic/workflows
// (Logic Apps, plan Consumption): CRUD síncrono del workflow, con un
// trigger Recurrence real que dispara automáticamente -- igual que
// gcp-emulator dispara de verdad un Cloud Scheduler job (Fase 11) -- y una
// única acción Http real que se invoca cuando el trigger dispara.
//
// Fase 21 (capa de comportamiento): a diferencia de las fases que solo
// modelan shape, un workflow con state "Enabled" y un trigger Recurrence
// válido (definition.triggers con una sola entrada de type "Recurrence")
// ahora dispara de verdad -- un goroutine por workflow calcula el próximo
// fire time con internal/cronlike y, si definition.actions tiene una sola
// acción de type "Http", hace un HTTP request real al URI configurado.
// Cualquier otro shape de trigger/acción se acepta igual (passthrough,
// nunca se rechaza el PUT por esto) pero simplemente no dispara nada real,
// el mismo principio que pubsubTarget en Cloud Scheduler o los receptores
// no-webhook en Action Groups.
package logicapps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/cronlike"
	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const workflowsBucket = "logicapps.workflows"

// Workflow replica el subconjunto relevante de Microsoft.Logic/workflows.
type Workflow struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	Location   string             `json:"location"`
	Tags       map[string]string  `json:"tags,omitempty"`
	Properties WorkflowProperties `json:"properties"`
}

// WorkflowProperties replica el subconjunto relevante de
// Microsoft.Logic/workflows#WorkflowProperties.
type WorkflowProperties struct {
	State             string          `json:"state"` // Enabled | Disabled
	ProvisioningState string          `json:"provisioningState"`
	CreatedTime       string          `json:"createdTime,omitempty"`
	ChangedTime       string          `json:"changedTime,omitempty"`
	Definition        json.RawMessage `json:"definition"`
	Parameters        json.RawMessage `json:"parameters,omitempty"`

	// LastRunTime/LastRunStatus son una extensión del emulador (no existen
	// en el shape real de Azure) para poder observar el resultado del
	// disparo real sin necesitar un endpoint de run history -- mismo patrón
	// que LastNotificationTime/Status en Monitor Action Groups (Fase 20) y
	// lastDeliveryStatus/Time en Event Grid (Fase 17). Los clientes reales
	// (az CLI/Terraform) ignoran campos desconocidos en la respuesta JSON.
	LastRunTime   string `json:"lastRunTime,omitempty"`
	LastRunStatus string `json:"lastRunStatus,omitempty"`
}

// workflowDefinition es la vista mínima de la Workflow Definition Language
// que necesitamos para encontrar el trigger/acción a disparar de verdad. El
// resto del documento (parameters, outputs, $schema...) se conserva tal
// cual en Properties.Definition (json.RawMessage) sin interpretarse.
type workflowDefinition struct {
	Triggers map[string]json.RawMessage `json:"triggers,omitempty"`
	Actions  map[string]json.RawMessage `json:"actions,omitempty"`
}

type recurrenceTriggerShape struct {
	Type       string              `json:"type"`
	Recurrence cronlike.Recurrence `json:"recurrence"`
}

type httpActionShape struct {
	Type   string `json:"type"`
	Inputs struct {
		Method  string            `json:"method"`
		URI     string            `json:"uri"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    json.RawMessage   `json:"body,omitempty"`
	} `json:"inputs"`
}

// findRecurrenceTrigger busca un trigger Recurrence disparable: el shape
// soportado es exactamente un trigger en definition.triggers cuyo "type"
// sea "Recurrence" (case-insensitive) con una "recurrence" válida según
// internal/cronlike. Cualquier otro shape (cero triggers, más de uno, o un
// type distinto) se acepta igual como passthrough pero no dispara nada.
func findRecurrenceTrigger(def workflowDefinition) (name string, rec cronlike.Recurrence, ok bool) {
	if len(def.Triggers) != 1 {
		return "", cronlike.Recurrence{}, false
	}
	for n, raw := range def.Triggers {
		var shape recurrenceTriggerShape
		if err := json.Unmarshal(raw, &shape); err != nil {
			return "", cronlike.Recurrence{}, false
		}
		if !strings.EqualFold(shape.Type, "Recurrence") {
			return "", cronlike.Recurrence{}, false
		}
		if err := shape.Recurrence.Validate(); err != nil {
			return "", cronlike.Recurrence{}, false
		}
		return n, shape.Recurrence, true
	}
	return "", cronlike.Recurrence{}, false
}

// findHTTPAction busca la única acción Http disparable: exactamente una
// entrada en definition.actions cuyo "type" sea "Http" con un uri no vacío.
// Cualquier otro shape se acepta igual como passthrough pero no se invoca.
func findHTTPAction(def workflowDefinition) (name string, action httpActionShape, ok bool) {
	if len(def.Actions) != 1 {
		return "", httpActionShape{}, false
	}
	for n, raw := range def.Actions {
		var shape httpActionShape
		if err := json.Unmarshal(raw, &shape); err != nil {
			return "", httpActionShape{}, false
		}
		if !strings.EqualFold(shape.Type, "Http") || strings.TrimSpace(shape.Inputs.URI) == "" {
			return "", httpActionShape{}, false
		}
		return n, shape, true
	}
	return "", httpActionShape{}, false
}

// Service agrupa el estado necesario para atender Logic Apps: la base de
// datos embebida, el cliente HTTP para el disparo real, y el registro de
// goroutines de disparo activos (uno por workflow habilitado).
type Service struct {
	db         *storage.DB
	httpClient *http.Client

	mu    sync.Mutex
	stops map[string]chan struct{} // workflow key -> stop signal
}

// New crea el servicio de Logic Apps y reanuda el disparo automático de
// cualquier workflow que ya estuviera "Enabled" con un trigger Recurrence
// válido antes de un reinicio -- el estado vive en BoltDB, pero los
// goroutines de disparo no, así que hay que relanzarlos aquí, igual que
// hace gcp-emulator con Cloud Scheduler en su propio New().
func New(db *storage.DB) *Service {
	s := &Service{
		db:         db,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stops:      make(map[string]chan struct{}),
	}
	_ = s.db.List(workflowsBucket, "", func(key string, raw []byte) error {
		var wf Workflow
		if err := json.Unmarshal(raw, &wf); err != nil {
			return err
		}
		if wf.Properties.State == "Enabled" {
			s.startFiring(key, wf)
		}
		return nil
	})
	return s
}

// startFiring lanza (o relanza) el goroutine de disparo de un workflow. No
// hace nada si no hay un trigger Recurrence disparable.
func (s *Service) startFiring(key string, wf Workflow) {
	var def workflowDefinition
	if err := json.Unmarshal(wf.Properties.Definition, &def); err != nil {
		return
	}
	_, rec, ok := findRecurrenceTrigger(def)
	if !ok {
		return
	}
	created := time.Now().UTC()
	if wf.Properties.CreatedTime != "" {
		if t, err := time.Parse(time.RFC3339, wf.Properties.CreatedTime); err == nil {
			created = t
		}
	}

	s.mu.Lock()
	if old, ok := s.stops[key]; ok {
		close(old)
	}
	stop := make(chan struct{})
	s.stops[key] = stop
	s.mu.Unlock()

	go s.fireLoop(key, created, rec, stop)
}

// stopFiring detiene el goroutine de disparo de un workflow, si existe
// (deshabilitar/borrar).
func (s *Service) stopFiring(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stop, ok := s.stops[key]; ok {
		close(stop)
		delete(s.stops, key)
	}
}

func (s *Service) fireLoop(key string, created time.Time, rec cronlike.Recurrence, stop chan struct{}) {
	for {
		next, err := cronlike.Next(rec, created, time.Now().UTC())
		if err != nil {
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
			s.dispatch(key)
		}
	}
}

// dispatch relee el workflow (puede haber cambiado desde que se programó el
// disparo), y si tiene una única acción Http disparable, hace un POST/GET/...
// HTTP real a su URI -- mismo patrón fire-and-forget con timeout corto, sin
// reintentos ni dead-lettering, que internal/services/monitor's
// ActionGroups.dispatch y gcp-emulator's Cloud Scheduler dispatch.
func (s *Service) dispatch(key string) {
	var wf Workflow
	found, err := s.db.Get(workflowsBucket, key, &wf)
	if err != nil || !found {
		return
	}

	var def workflowDefinition
	if err := json.Unmarshal(wf.Properties.Definition, &def); err != nil {
		return
	}

	status := "skipped: no dispatchable Http action configured"
	if _, action, ok := findHTTPAction(def); ok {
		method := action.Inputs.Method
		if method == "" {
			method = "POST"
		}
		var body []byte
		if len(action.Inputs.Body) > 0 {
			body = []byte(action.Inputs.Body)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, rerr := http.NewRequestWithContext(ctx, method, action.Inputs.URI, bytes.NewReader(body))
		if rerr != nil {
			status = rerr.Error()
		} else {
			for k, v := range action.Inputs.Headers {
				req.Header.Set(k, v)
			}
			if action.Inputs.Body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, derr := s.httpClient.Do(req)
			if derr != nil {
				status = derr.Error()
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					status = fmt.Sprintf("http %d", resp.StatusCode)
				} else {
					status = "ok"
				}
			}
		}
		cancel()
	}

	wf.Properties.LastRunTime = time.Now().UTC().Format(time.RFC3339)
	wf.Properties.LastRunStatus = status
	_ = s.db.Put(workflowsBucket, key, wf)
}

type workflowRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		State      string          `json:"state"`
		Definition json.RawMessage `json:"definition"`
		Parameters json.RawMessage `json:"parameters,omitempty"`
	} `json:"properties"`
}

func workflowKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func workflowID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Logic/workflows/%s", subID, rg, name)
}

// Register monta las rutas de Logic Apps.
func (s *Service) Register(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Logic/workflows"
	mux.HandleFunc("GET "+base, s.listWorkflows)
	mux.HandleFunc("PUT "+base+"/{workflowName}", s.putWorkflow)
	mux.HandleFunc("GET "+base+"/{workflowName}", s.getWorkflow)
	mux.HandleFunc("DELETE "+base+"/{workflowName}", s.deleteWorkflow)
	mux.HandleFunc("GET "+base+"/{workflowName}/triggers", s.listTriggers)
	mux.HandleFunc("GET "+base+"/{workflowName}/triggers/{triggerName}", s.getTrigger)
	// El disparo manual de un trigger ("/run") es la acción real que az CLI
	// expone como `az logic workflow trigger run`, igual que el ":run" de un
	// Cloud Scheduler job en gcp-emulator -- a diferencia de ese, Azure usa
	// un sub-path literal en vez de "{name}:action", mismo patrón que
	// createNotifications en Monitor Action Groups.
	mux.HandleFunc("POST "+base+"/{workflowName}/triggers/{triggerName}/run", s.runTrigger)
}

func (s *Service) putWorkflow(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")

	var req workflowRequest
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
	if len(req.Properties.Definition) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.definition' es obligatorio (Workflow Definition Language)")
		return
	}

	state := req.Properties.State
	if state == "" {
		state = "Enabled"
	}

	key := workflowKey(subID, rg, name)
	var existing Workflow
	found, err := s.db.Get(workflowsBucket, key, &existing)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	createdTime := now
	lastRunTime, lastRunStatus := "", ""
	if found {
		createdTime = existing.Properties.CreatedTime
		lastRunTime = existing.Properties.LastRunTime
		lastRunStatus = existing.Properties.LastRunStatus
	}

	wf := Workflow{
		ID:       workflowID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Logic/workflows",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: WorkflowProperties{
			State:             state,
			ProvisioningState: "Succeeded",
			CreatedTime:       createdTime,
			ChangedTime:       now,
			Definition:        req.Properties.Definition,
			Parameters:        req.Properties.Parameters,
			LastRunTime:       lastRunTime,
			LastRunStatus:     lastRunStatus,
		},
	}

	if err := s.db.Put(workflowsBucket, key, wf); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if state == "Enabled" {
		s.startFiring(key, wf)
	} else {
		s.stopFiring(key)
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, wf)
}

func (s *Service) getWorkflowByKey(subID, rg, name string) (Workflow, bool, error) {
	var wf Workflow
	found, err := s.db.Get(workflowsBucket, workflowKey(subID, rg, name), &wf)
	return wf, found, err
}

func (s *Service) getWorkflow(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")

	wf, found, err := s.getWorkflowByKey(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workflow '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, wf)
}

func (s *Service) listWorkflows(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	workflows := make([]Workflow, 0)
	err := s.db.List(workflowsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var wf Workflow
		if err := json.Unmarshal(raw, &wf); err != nil {
			return err
		}
		workflows = append(workflows, wf)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": workflows})
}

// deleteWorkflow es idempotente (204 si no existe), igual que deleteVault y
// deleteActionGroup.
func (s *Service) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")
	key := workflowKey(subID, rg, name)

	found, err := s.db.Get(workflowsBucket, key, &Workflow{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(workflowsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	s.stopFiring(key)
	w.WriteHeader(http.StatusOK)
}

// triggerInfo replica el subconjunto mínimo de WorkflowTrigger que az
// CLI/Terraform podrían leer al listar/obtener un trigger.
type triggerInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	State      string `json:"state"`
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
	} `json:"properties"`
}

func (s *Service) triggersOf(wf Workflow) []triggerInfo {
	var def workflowDefinition
	if err := json.Unmarshal(wf.Properties.Definition, &def); err != nil {
		return nil
	}
	triggers := make([]triggerInfo, 0, len(def.Triggers))
	for name, raw := range def.Triggers {
		var shape struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &shape)
		ti := triggerInfo{Name: name, Type: shape.Type, State: wf.Properties.State}
		ti.Properties.ProvisioningState = "Succeeded"
		triggers = append(triggers, ti)
	}
	return triggers
}

func (s *Service) listTriggers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")

	wf, found, err := s.getWorkflowByKey(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workflow '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": s.triggersOf(wf)})
}

func (s *Service) getTrigger(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")
	triggerName := r.PathValue("triggerName")

	wf, found, err := s.getWorkflowByKey(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workflow '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	for _, ti := range s.triggersOf(wf) {
		if ti.Name == triggerName {
			server.WriteJSON(w, http.StatusOK, ti)
			return
		}
	}
	server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("el trigger '%s' no existe en el workflow '%s'", triggerName, name))
}

// runTrigger dispara el trigger manualmente -- equivalente real de
// `az logic workflow trigger run`. A diferencia del disparo automático por
// recurrencia (fire-and-forget desde el goroutine), este corre el dispatch
// de forma síncrona y devuelve el workflow actualizado (con
// lastRunTime/lastRunStatus ya reflejando el resultado), lo que hace que el
// disparo manual sea determinístico y fácil de verificar en smoke tests --
// Azure real responde 202 sin cuerpo y expone el resultado vía run history,
// que este emulador no implementa.
func (s *Service) runTrigger(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workflowName")
	triggerName := r.PathValue("triggerName")
	key := workflowKey(subID, rg, name)

	wf, found, err := s.getWorkflowByKey(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workflow '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	exists := false
	for _, ti := range s.triggersOf(wf) {
		if ti.Name == triggerName {
			exists = true
			break
		}
	}
	if !exists {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el trigger '%s' no existe en el workflow '%s'", triggerName, name))
		return
	}

	s.dispatch(key)

	wf, _, err = s.getWorkflowByKey(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, wf)
}
