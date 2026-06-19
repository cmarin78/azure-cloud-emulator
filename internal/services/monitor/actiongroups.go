package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const actionGroupsBucket = "monitor.actionGroups"

// ActionGroup replica el subconjunto relevante de Microsoft.Insights/
// actionGroups. Los distintos tipos de receiver (email/sms/webhook/...) se
// mantienen como json.RawMessage porque su estructura interna no es
// relevante para el emulador -- no se envía ninguna notificación real, solo
// se persisten y se devuelven tal cual, mismo enfoque que gcp-emulator usa
// para "conditions" en monitoring.AlertPolicy.
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
