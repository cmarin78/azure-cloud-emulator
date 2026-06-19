package monitor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const workspacesBucket = "monitor.workspaces"

// Sku replica "properties.sku" de un workspace (p. ej. {"name": "PerGB2018"}).
type Sku struct {
	Name string `json:"name"`
}

// Workspace replica el subconjunto relevante de
// Microsoft.OperationalInsights/workspaces que az/Terraform leen.
type Workspace struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties WorkspaceProperties `json:"properties"`
}

type WorkspaceProperties struct {
	ProvisioningState               string `json:"provisioningState"`
	Sku                             Sku    `json:"sku"`
	RetentionInDays                 int    `json:"retentionInDays"`
	CustomerID                      string `json:"customerId"`
	PublicNetworkAccessForIngestion string `json:"publicNetworkAccessForIngestion"`
	PublicNetworkAccessForQuery     string `json:"publicNetworkAccessForQuery"`
}

type workspaceRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		Sku             Sku `json:"sku"`
		RetentionInDays int `json:"retentionInDays"`
	} `json:"properties"`
}

func workspaceKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func workspaceID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.OperationalInsights/workspaces/%s", subID, rg, name)
}

// fakeCustomerID deriva un GUID estable a partir de la identidad del
// workspace (subscripción + resource group + nombre), para que az/Terraform
// vean siempre el mismo "workspace ID" entre reinicios del emulador --
// mismo truco que graph.fakeObjectID.
func fakeCustomerID(subID, rg, name string) string {
	sum := sha256.Sum256([]byte("azure-emulator-fake-customer-id:" + workspaceKey(subID, rg, name)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func (s *Service) registerWorkspaces(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.OperationalInsights/workspaces"
	mux.HandleFunc("GET "+base, s.listWorkspaces)
	mux.HandleFunc("PUT "+base+"/{workspaceName}", s.putWorkspace)
	mux.HandleFunc("GET "+base+"/{workspaceName}", s.getWorkspaceHandler)
	mux.HandleFunc("DELETE "+base+"/{workspaceName}", s.deleteWorkspace)
	mux.HandleFunc("POST "+base+"/{workspaceName}/sharedKeys", s.workspaceSharedKeys)
}

func (s *Service) putWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workspaceName")

	var req workspaceRequest
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

	skuName := req.Properties.Sku.Name
	if strings.TrimSpace(skuName) == "" {
		skuName = "PerGB2018"
	}
	retention := req.Properties.RetentionInDays
	if retention == 0 {
		retention = 30
	}

	key := workspaceKey(subID, rg, name)
	_, found, err := s.getWorkspace(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	ws := Workspace{
		ID:       workspaceID(subID, rg, name),
		Name:     name,
		Type:     "Microsoft.OperationalInsights/workspaces",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: WorkspaceProperties{
			ProvisioningState:               "Succeeded",
			Sku:                             Sku{Name: skuName},
			RetentionInDays:                 retention,
			CustomerID:                      fakeCustomerID(subID, rg, name),
			PublicNetworkAccessForIngestion: "Enabled",
			PublicNetworkAccessForQuery:     "Enabled",
		},
	}

	if err := s.db.Put(workspacesBucket, key, ws); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, ws)
}

func (s *Service) getWorkspace(subID, rg, name string) (Workspace, bool, error) {
	var ws Workspace
	found, err := s.db.Get(workspacesBucket, workspaceKey(subID, rg, name), &ws)
	return ws, found, err
}

func (s *Service) getWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workspaceName")

	ws, found, err := s.getWorkspace(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workspace '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, ws)
}

func (s *Service) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	workspaces := make([]Workspace, 0)
	err := s.db.List(workspacesBucket, subID+"/"+rg+"/", func(key string, raw []byte) error {
		var ws Workspace
		if err := json.Unmarshal(raw, &ws); err != nil {
			return err
		}
		workspaces = append(workspaces, ws)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": workspaces})
}

// deleteWorkspace es idempotente (204 si no existe) y síncrono, igual que
// deleteVault: no cascada-borra ningún dato del stub de query (no hay datos
// reales de logs que borrar).
func (s *Service) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workspaceName")
	key := workspaceKey(subID, rg, name)

	found, err := s.db.Get(workspacesBucket, key, &Workspace{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(workspacesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// workspaceSharedKeys emula la acción POST .../sharedKeys que
// azurerm_log_analytics_workspace usa para leer primary_shared_key/
// secondary_shared_key. No hay clave real detrás: son bytes aleatorios
// codificados en base64, generados en cada llamada (igual que Azure real,
// que también permite regenerarlas), así que no se persisten.
func (s *Service) workspaceSharedKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("workspaceName")

	if _, found, err := s.getWorkspace(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	} else if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("el workspace '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	primary, err := randomKey()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	secondary, err := randomKey()
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]string{
		"primarySharedKey":   primary,
		"secondarySharedKey": secondary,
	})
}

func randomKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
