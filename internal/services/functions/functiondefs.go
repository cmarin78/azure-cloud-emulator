package functions

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const functionsBucket = "functions.definitions"

// FunctionDefinition replica el subconjunto relevante de
// Microsoft.Web/sites/functions que `az functionapp function list/show`
// lee. "config" se persiste tal cual (json.RawMessage), igual que
// monitor.MetricAlert.Criteria — nadie en este emulador evalúa los
// bindings reales de function.json.
type FunctionDefinition struct {
	ID         string                       `json:"id"`
	Name       string                       `json:"name"`
	Type       string                       `json:"type"`
	Properties FunctionDefinitionProperties `json:"properties"`
}

type FunctionDefinitionProperties struct {
	Name              string          `json:"name"`
	Language          string          `json:"language,omitempty"`
	IsDisabled        bool            `json:"isDisabled"`
	Config            json.RawMessage `json:"config,omitempty"`
	InvokeURLTemplate string          `json:"invoke_url_template,omitempty"`
}

type functionDefinitionRequest struct {
	Properties struct {
		Language   string          `json:"language,omitempty"`
		IsDisabled *bool           `json:"isDisabled,omitempty"`
		Config     json.RawMessage `json:"config,omitempty"`
	} `json:"properties"`
}

func (s *Service) registerFunctions(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Web/sites/{siteName}"
	mux.HandleFunc("GET "+base+"/functions", s.listFunctions)
	mux.HandleFunc("PUT "+base+"/functions/{functionName}", s.putFunction)
	mux.HandleFunc("GET "+base+"/functions/{functionName}", s.getFunctionHandler)
	mux.HandleFunc("DELETE "+base+"/functions/{functionName}", s.deleteFunction)
	mux.HandleFunc("POST "+base+"/syncfunctiontriggers", s.syncFunctionTriggers)
	mux.HandleFunc("POST "+base+"/host/default/listkeys", s.listHostKeys)
}

func functionKey(subID, rg, site, name string) string {
	return subID + "/" + rg + "/" + site + "/" + name
}

func functionID(subID, rg, site, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s/functions/%s", subID, rg, site, name)
}

// invokeURLTemplate deriva la URL pública "creíble" de la función,
// siguiendo el mismo patrón fake-pero-estable que
// appservice.defaultHostName (que a su vez sirve de base aquí).
func invokeURLTemplate(site, name string) string {
	return fmt.Sprintf("https://%s.azurewebsites.net/api/%s", site, name)
}

// putFunction es síncrono (mismo Effort "S" que appservice's config/
// appsettings): no hay despliegue real de código detrás, solo se persiste
// el shape de la definición. No valida que el site padre exista — mismo
// enfoque "sin integridad referencial estricta" usado en todo el proyecto
// (monitor.actionGroupId, appservice.serverFarmId, network's NSG/RouteTable
// en subnets).
func (s *Service) putFunction(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	site := r.PathValue("siteName")
	name := r.PathValue("functionName")

	var req functionDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	if len(req.Properties.Config) == 0 {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter",
			"el campo 'properties.config' es obligatorio")
		return
	}
	isDisabled := false
	if req.Properties.IsDisabled != nil {
		isDisabled = *req.Properties.IsDisabled
	}

	key := functionKey(subID, rg, site, name)
	_, found, err := s.getFunction(subID, rg, site, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	def := FunctionDefinition{
		ID:   functionID(subID, rg, site, name),
		Name: site + "/" + name,
		Type: "Microsoft.Web/sites/functions",
		Properties: FunctionDefinitionProperties{
			Name:              name,
			Language:          req.Properties.Language,
			IsDisabled:        isDisabled,
			Config:            req.Properties.Config,
			InvokeURLTemplate: invokeURLTemplate(site, name),
		},
	}

	if err := s.db.Put(functionsBucket, key, def); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, def)
}

func (s *Service) getFunction(subID, rg, site, name string) (FunctionDefinition, bool, error) {
	var def FunctionDefinition
	found, err := s.db.Get(functionsBucket, functionKey(subID, rg, site, name), &def)
	return def, found, err
}

func (s *Service) getFunctionHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	site := r.PathValue("siteName")
	name := r.PathValue("functionName")

	def, found, err := s.getFunction(subID, rg, site, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la función '%s' no existe en el function app '%s'", name, site))
		return
	}
	server.WriteJSON(w, http.StatusOK, def)
}

func (s *Service) listFunctions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	site := r.PathValue("siteName")

	defs := make([]FunctionDefinition, 0)
	err := s.db.List(functionsBucket, subID+"/"+rg+"/"+site+"/", func(key string, raw []byte) error {
		var def FunctionDefinition
		if err := json.Unmarshal(raw, &def); err != nil {
			return err
		}
		defs = append(defs, def)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": defs})
}

// deleteFunction es idempotente (204 si no existe), mismo patrón que el
// resto de los sub-recursos síncronos del proyecto.
func (s *Service) deleteFunction(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	site := r.PathValue("siteName")
	name := r.PathValue("functionName")
	key := functionKey(subID, rg, site, name)

	found, err := s.db.Get(functionsBucket, key, &FunctionDefinition{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(functionsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// syncFunctionTriggers emula la acción que `func azure functionapp publish`/
// az CLI invocan tras desplegar código para que el host recargue sus
// triggers. No hay nada que recargar de verdad — siempre 204, igual que el
// resto de las acciones sync-sin-cuerpo del proyecto (p. ej.
// appservice start/stop/restart, salvo que esas sí devuelven 200 con
// cuerpo vacío; aquí se sigue el shape real de Azure, que es 204 sin
// cuerpo).
func (s *Service) syncFunctionTriggers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listHostKeys emula POST .../host/default/listkeys, que az
// functionapp keys list / herramientas de despliegue usan para firmar URLs
// de invocación con triggers HTTP "function"-level auth. La masterKey es
// determinista (FNV-32a del nombre completo del site), mismo espíritu que
// fakePublicIP en network/publicip.go y el fqdn/identity de aks/cluster.go.
func (s *Service) listHostKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	site := r.PathValue("siteName")

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"masterKey": fakeHostKey(subID + "/" + rg + "/" + site + "/master"),
		"functionKeys": map[string]string{
			"default": fakeHostKey(subID + "/" + rg + "/" + site + "/default"),
		},
		"systemKeys": map[string]string{},
	})
}

func fakeHostKey(seed string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return fmt.Sprintf("%08x-fake-host-key", h.Sum32())
}
