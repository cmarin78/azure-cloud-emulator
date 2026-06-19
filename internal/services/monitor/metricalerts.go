package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const metricAlertsBucket = "monitor.metricAlerts"

// MetricAlert replica el subconjunto relevante de Microsoft.Insights/
// metricAlerts. Como con ActionGroup, "criteria" y "scopes" se persisten tal
// cual (json.RawMessage / []string) sin evaluarse nunca -- no hay pipeline
// de métricas real detrás, igual que gcp-emulator nunca evalúa
// monitoring.AlertPolicy.Conditions.
type MetricAlert struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Location   string                `json:"location"`
	Tags       map[string]string     `json:"tags,omitempty"`
	Properties MetricAlertProperties `json:"properties"`
}

type MetricAlertProperties struct {
	Description         string          `json:"description,omitempty"`
	Severity            int             `json:"severity"`
	Enabled             bool            `json:"enabled"`
	Scopes              []string        `json:"scopes"`
	EvaluationFrequency string          `json:"evaluationFrequency"`
	WindowSize          string          `json:"windowSize"`
	Criteria            json.RawMessage `json:"criteria"`
	AutoMitigate        bool            `json:"autoMitigate"`
	Actions             json.RawMessage `json:"actions,omitempty"`
}

type metricAlertRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		Description         string          `json:"description"`
		Severity            *int            `json:"severity"`
		Enabled             *bool           `json:"enabled"`
		Scopes              []string        `json:"scopes"`
		EvaluationFrequency string          `json:"evaluationFrequency"`
		WindowSize          string          `json:"windowSize"`
		Criteria            json.RawMessage `json:"criteria"`
		AutoMitigate        *bool           `json:"autoMitigate"`
		Actions             json.RawMessage `json:"actions"`
	} `json:"properties"`
}

func metricAlertKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func metricAlertID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/metricAlerts/%s", subID, rg, name)
}

func (s *Service) registerMetricAlerts(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Insights/metricAlerts"
	mux.HandleFunc("GET "+base, s.listMetricAlerts)
	mux.HandleFunc("PUT "+base+"/{alertName}", s.putMetricAlert)
	mux.HandleFunc("GET "+base+"/{alertName}", s.getMetricAlertHandler)
	mux.HandleFunc("DELETE "+base+"/{alertName}", s.deleteMetricAlert)
}

func (s *Service) putMetricAlert(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("alertName")

	var req metricAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'location' es obligatorio (azurerm_monitor_metric_alert usa 'global')")
		return
	}
	if len(req.Properties.Scopes) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.scopes' es obligatorio y debe tener al menos un elemento")
		return
	}

	severity := 3
	if req.Properties.Severity != nil {
		severity = *req.Properties.Severity
	}
	enabled := true
	if req.Properties.Enabled != nil {
		enabled = *req.Properties.Enabled
	}
	autoMitigate := true
	if req.Properties.AutoMitigate != nil {
		autoMitigate = *req.Properties.AutoMitigate
	}
	evalFreq := req.Properties.EvaluationFrequency
	if strings.TrimSpace(evalFreq) == "" {
		evalFreq = "PT5M"
	}
	windowSize := req.Properties.WindowSize
	if strings.TrimSpace(windowSize) == "" {
		windowSize = "PT15M"
	}

	key := metricAlertKey(subID, rg, name)
	_, found, err := s.getMetricAlert(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	alert := MetricAlert{
		ID:       metricAlertID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.Insights/metricAlerts",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: MetricAlertProperties{
			Description:         req.Properties.Description,
			Severity:            severity,
			Enabled:             enabled,
			Scopes:              req.Properties.Scopes,
			EvaluationFrequency: evalFreq,
			WindowSize:          windowSize,
			Criteria:            req.Properties.Criteria,
			AutoMitigate:        autoMitigate,
			Actions:             req.Properties.Actions,
		},
	}

	if err := s.db.Put(metricAlertsBucket, key, alert); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, alert)
}

func (s *Service) getMetricAlert(subID, rg, name string) (MetricAlert, bool, error) {
	var alert MetricAlert
	found, err := s.db.Get(metricAlertsBucket, metricAlertKey(subID, rg, name), &alert)
	return alert, found, err
}

func (s *Service) getMetricAlertHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("alertName")

	alert, found, err := s.getMetricAlert(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el metric alert '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, alert)
}

func (s *Service) listMetricAlerts(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	alerts := make([]MetricAlert, 0)
	err := s.db.List(metricAlertsBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var alert MetricAlert
		if err := json.Unmarshal(raw, &alert); err != nil {
			return err
		}
		alerts = append(alerts, alert)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": alerts})
}

// deleteMetricAlert es idempotente (204 si no existe), igual que deleteVault.
func (s *Service) deleteMetricAlert(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("alertName")
	key := metricAlertKey(subID, rg, name)

	found, err := s.db.Get(metricAlertsBucket, key, &MetricAlert{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(metricAlertsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
