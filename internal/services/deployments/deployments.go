// Package deployments emula Microsoft.Resources/deployments: a diferencia
// del resto de los servicios de este emulador, no modela un único tipo de
// recurso sino un dispatcher que recibe un template ARM completo (JSON
// nativo, o el JSON ya compilado de un archivo Bicep — son el mismo shape)
// y crea cada uno de los recursos que declara reenviando una solicitud PUT
// sintética al mux principal del emulador, exactamente con el mismo path
// y body que un cliente real usaría para crear ese recurso uno por uno.
// Así se evita reimplementar la lógica de creación de cada tipo de
// recurso — este paquete solo resuelve expresiones ARM (parameters(),
// variables(), resourceId() — ver template.go para el alcance
// deliberadamente incompleto), ordena por dependsOn, y despacha.
package deployments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	deploymentsBucket = "deployments.deployments"
	operationsBucket  = "deployments.operations"
	resourcesProvider = "Microsoft.Resources"
)

// Service agrupa el estado necesario para atender las rutas de
// deployments: la base de datos, el registro de operaciones async
// compartido, y un http.Handler — el mux principal del emulador — usado
// para despachar cada recurso del template como si fuera una solicitud
// PUT real entrante. Se acepta como http.Handler (no *http.ServeMux)
// para que los tests puedan inyectar un mux mínimo sin depender de todos
// los demás servicios.
type Service struct {
	db  *storage.DB
	ops *server.Operations
	mux http.Handler
}

// New crea el servicio de deployments. mux debe ser el mismo
// http.ServeMux donde están registrados (o se registrarán) todos los
// demás servicios — Register solo añade las rutas de
// Microsoft.Resources/deployments, no reemplaza nada.
func New(db *storage.DB, ops *server.Operations, mux http.Handler) *Service {
	return &Service{db: db, ops: ops, mux: mux}
}

// Register monta las rutas de Microsoft.Resources/deployments en mux.
// Nótese que el mux aquí (el parámetro de Register) y s.mux (usado para
// despachar) suelen ser el mismo *http.ServeMux: en main.go este servicio
// se registra al final, después de todos los demás, así que para cuando
// el servidor empieza a atender requests todas las rutas ya están
// montadas — el orden de registro no afecta el ruteo de
// http.ServeMux.ServeHTTP.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("PUT /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Resources/deployments/{deploymentName}",
		s.putDeployment)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Resources/deployments/{deploymentName}",
		s.getDeployment)
	mux.HandleFunc("DELETE /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Resources/deployments/{deploymentName}",
		s.deleteDeployment)
	mux.HandleFunc("GET /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Resources/deployments/{deploymentName}/operations",
		s.listDeploymentOperations)
	mux.HandleFunc("POST /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Resources/deployments/{deploymentName}/validate",
		s.validateDeployment)
}

// Deployment replica el shape mínimo de Microsoft.Resources/deployments
// que `az deployment group create/show` y
// azurerm_resource_group_template_deployment leen.
type Deployment struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Properties DeploymentProperties `json:"properties"`
}

type DeploymentProperties struct {
	ProvisioningState string           `json:"provisioningState"`
	Mode              string           `json:"mode"`
	Timestamp         string           `json:"timestamp"`
	Duration          string           `json:"duration"`
	Outputs           map[string]any   `json:"outputs"`
	OutputResources   []OutputResource `json:"outputResources"`
	Error             *server.APIError `json:"error,omitempty"`
}

type OutputResource struct {
	ID string `json:"id"`
}

// DeploymentOperation replica una entrada de
// deployments/{name}/operations — una por recurso despachado.
type DeploymentOperation struct {
	ID          string                        `json:"id"`
	OperationID string                        `json:"operationId"`
	Properties  DeploymentOperationProperties `json:"properties"`
}

type DeploymentOperationProperties struct {
	ProvisioningOperation string          `json:"provisioningOperation"`
	ProvisioningState     string          `json:"provisioningState"`
	Timestamp             string          `json:"timestamp"`
	Duration              string          `json:"duration"`
	TargetResource        TargetResource  `json:"targetResource"`
	StatusCode            string          `json:"statusCode"`
	StatusMessage         json.RawMessage `json:"statusMessage,omitempty"`
}

type TargetResource struct {
	ID           string `json:"id"`
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
}

type deploymentParameterValue struct {
	Value json.RawMessage `json:"value"`
}

type deploymentRequest struct {
	Properties struct {
		Mode       string                              `json:"mode,omitempty"`
		Template   template                            `json:"template"`
		Parameters map[string]deploymentParameterValue `json:"parameters,omitempty"`
	} `json:"properties"`
}

func deploymentKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

func deploymentID(subID, rg, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Resources/deployments/%s", subID, rg, name)
}

// putDeployment resuelve el template completo (parameters/variables,
// expresiones ARM, orden por dependsOn) y despacha cada recurso
// resultante como una solicitud PUT sintética contra s.mux. Es async en
// el shape (responde con Azure-AsyncOperation/Location) pero, como el
// resto de los servicios de este emulador, procesa todo de forma
// síncrona antes de responder — con la diferencia de que, a diferencia
// de AKS/API Management, el deployment sí puede terminar en "Failed" de
// verdad si alguno de los recursos que despacha responde con un error,
// ya que cada recurso despachado corre la validación real de su propio
// servicio.
func (s *Service) putDeployment(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("deploymentName")

	var req deploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}
	mode := req.Properties.Mode
	if mode == "" {
		mode = "Incremental"
	}

	_, existed, _ := s.getDeploymentRecord(subID, rg, name)

	ctx := &evalContext{subID: subID, rg: rg}
	params, err := resolveParameters(&req.Properties.Template, req.Properties.Parameters)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	ctx.parameters = params
	if err := resolveVariables(&req.Properties.Template, ctx); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	resolved, err := resolveResources(&req.Properties.Template, ctx)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	ordered, err := orderByDependencies(resolved, ctx)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "DeploymentCycleDetected", err.Error())
		return
	}

	deployment := Deployment{
		ID:   deploymentID(subID, rg, name),
		Name: name,
		Type: "Microsoft.Resources/deployments",
		Properties: DeploymentProperties{
			Mode:      mode,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Duration:  "PT0S",
			Outputs:   map[string]any{},
		},
	}

	operations, failedAt := s.dispatchResources(subID, rg, ordered)
	// Si este deployment ya existía (re-apply), limpiamos sus operations
	// anteriores antes de persistir las nuevas — si no, una redeploy con
	// menos recursos que la anterior dejaría entradas obsoletas mezcladas
	// con las nuevas en /operations.
	if existed {
		if err := s.clearOperations(subID, rg, name); err != nil {
			server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}
	if err := s.persistOperations(subID, rg, name, operations); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	outputResources := make([]OutputResource, 0, len(ordered))
	for _, r := range ordered {
		outputResources = append(outputResources, OutputResource{ID: r.path})
	}
	deployment.Properties.OutputResources = outputResources

	var opID string
	if failedAt >= 0 {
		deployment.Properties.ProvisioningState = "Failed"
		deployment.Properties.Error = operations[failedAt].statusError
		opID = s.ops.Failed(deployment.Properties.Error.Code, deployment.Properties.Error.Message)
	} else {
		deployment.Properties.ProvisioningState = "Succeeded"
		opID = s.ops.Succeeded()
	}

	if err := s.db.Put(deploymentsBucket, deploymentKey(subID, rg, name), deployment); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	url := server.AsyncOperationURL(r, subID, resourcesProvider, "global", opID, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, deployment)
}

// dispatchedOperation es el resultado interno de despachar un recurso del
// template — se usa tanto para construir DeploymentOperation (la vista
// pública) como para decidir si el deployment completo falló.
type dispatchedOperation struct {
	op          DeploymentOperation
	statusError *server.APIError
}

// dispatchResources reenvía cada recurso ya resuelto como una solicitud
// PUT sintética contra s.mux (in-process, sin red real — el mismo mux
// que atiende las solicitudes reales de az CLI/Terraform), en el orden
// recibido (que orderByDependencies ya ordenó por dependsOn). Se detiene
// en el primer recurso que falla (modo Incremental real: los recursos no
// procesados quedan tal como estaban) y devuelve el índice de la
// operación fallida, o -1 si todas tuvieron éxito.
func (s *Service) dispatchResources(subID, rg string, resources []resolvedResource) ([]dispatchedOperation, int) {
	results := make([]dispatchedOperation, 0, len(resources))
	failedAt := -1
	for i, res := range resources {
		bodyMap := map[string]any{}
		if res.location != "" {
			bodyMap["location"] = res.location
		}
		if len(res.original.Tags) > 0 {
			bodyMap["tags"] = res.original.Tags
		}
		if len(res.sku) > 0 {
			bodyMap["sku"] = res.sku
		}
		if len(res.identity) > 0 {
			bodyMap["identity"] = res.identity
		}
		if res.original.Kind != "" {
			bodyMap["kind"] = res.original.Kind
		}
		if len(res.properties) > 0 {
			bodyMap["properties"] = res.properties
		}
		body, _ := json.Marshal(bodyMap)

		target := TargetResource{ID: res.path, ResourceType: res.resType, ResourceName: res.name}
		now := time.Now().UTC().Format(time.RFC3339)

		req := httptest.NewRequest(http.MethodPut, res.path+"?api-version="+res.original.APIVersion, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)

		entry := dispatchedOperation{op: DeploymentOperation{
			OperationID: fmt.Sprintf("%d", i+1),
			Properties: DeploymentOperationProperties{
				ProvisioningOperation: "Create",
				Timestamp:             now,
				Duration:              "PT0S",
				TargetResource:        target,
				StatusCode:            fmt.Sprintf("%d", rec.Code),
			},
		}}

		if rec.Code >= 200 && rec.Code < 300 {
			entry.op.Properties.ProvisioningState = "Succeeded"
		} else {
			entry.op.Properties.ProvisioningState = "Failed"
			apiErr := parseAPIError(rec.Body.Bytes())
			entry.statusError = apiErr
			entry.op.Properties.StatusMessage = json.RawMessage(rec.Body.Bytes())
			results = append(results, entry)
			failedAt = i
			break
		}
		results = append(results, entry)
	}
	return results, failedAt
}

// parseAPIError intenta decodificar el cuerpo de error estándar de ARM
// ({"error":{"code","message"}}) que server.WriteError produce; si el
// cuerpo no tiene ese shape (por ejemplo un 404 genérico de net/http),
// devuelve un error genérico para no dejar el campo vacío.
func parseAPIError(body []byte) *server.APIError {
	var wrapper struct {
		Error server.APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Error.Code != "" {
		return &wrapper.Error
	}
	return &server.APIError{Code: "DeploymentResourceFailed", Message: "el recurso despachado por el deployment respondió con un error"}
}

func (s *Service) persistOperations(subID, rg, name string, ops []dispatchedOperation) error {
	for i, o := range ops {
		o.op.ID = fmt.Sprintf("%s/operations/%d", deploymentID(subID, rg, name), i+1)
		key := deploymentKey(subID, rg, name) + "/" + fmt.Sprintf("%04d", i+1)
		if err := s.db.Put(operationsBucket, key, o.op); err != nil {
			return err
		}
	}
	return nil
}

// clearOperations borra todas las entradas de operations persistidas para
// un deployment previo, usadas justo antes de persistir las del run
// actual en un re-apply (ver comentario en putDeployment).
func (s *Service) clearOperations(subID, rg, name string) error {
	prefix := deploymentKey(subID, rg, name) + "/"
	var keys []string
	if err := s.db.List(operationsBucket, prefix, func(key string, raw []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.db.Delete(operationsBucket, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) getDeploymentRecord(subID, rg, name string) (Deployment, bool, error) {
	var d Deployment
	found, err := s.db.Get(deploymentsBucket, deploymentKey(subID, rg, name), &d)
	return d, found, err
}

func (s *Service) getDeployment(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("deploymentName")

	d, found, err := s.getDeploymentRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "DeploymentNotFound",
			fmt.Sprintf("el deployment '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, d)
}

func (s *Service) listDeploymentOperations(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("deploymentName")

	prefix := deploymentKey(subID, rg, name) + "/"
	ops := make([]DeploymentOperation, 0)
	err := s.db.List(operationsBucket, prefix, func(key string, raw []byte) error {
		var o DeploymentOperation
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		ops = append(ops, o)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": ops})
}

// validateDeployment emula POST .../deployments/{name}/validate: resuelve
// el template (igual que putDeployment) para detectar errores de
// sintaxis/expresiones, pero nunca despacha ningún recurso ni persiste
// nada — es un stub de solo-forma, ya que el emulador no tiene reglas de
// validación reales que aplicar (cuotas, políticas, etc.), tal como
// documenta ROADMAP.md para esta fase.
func (s *Service) validateDeployment(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("deploymentName")

	var req deploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	ctx := &evalContext{subID: subID, rg: rg}
	params, err := resolveParameters(&req.Properties.Template, req.Properties.Parameters)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	ctx.parameters = params
	if err := resolveVariables(&req.Properties.Template, ctx); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	resolved, err := resolveResources(&req.Properties.Template, ctx)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidTemplate", err.Error())
		return
	}
	if _, err := orderByDependencies(resolved, ctx); err != nil {
		server.WriteError(w, http.StatusBadRequest, "DeploymentCycleDetected", err.Error())
		return
	}

	mode := req.Properties.Mode
	if mode == "" {
		mode = "Incremental"
	}
	outputResources := make([]OutputResource, 0, len(resolved))
	for _, res := range resolved {
		outputResources = append(outputResources, OutputResource{ID: res.path})
	}

	server.WriteJSON(w, http.StatusOK, Deployment{
		ID:   deploymentID(subID, rg, name),
		Name: name,
		Type: "Microsoft.Resources/deployments",
		Properties: DeploymentProperties{
			ProvisioningState: "Succeeded",
			Mode:              mode,
			Timestamp:         time.Now().UTC().Format(time.RFC3339),
			Duration:          "PT0S",
			Outputs:           map[string]any{},
			OutputResources:   outputResources,
		},
	})
}

// deleteDeployment borra solo el registro del deployment (y sus
// operations) — igual que Azure real, NO borra los recursos que ese
// deployment creó. Idempotente (204 si ya no existía), async en el shape
// como el resto de los deletes del proyecto.
func (s *Service) deleteDeployment(w http.ResponseWriter, r *http.Request) {
	apiVersion, ok := server.RequireAPIVersion(w, r)
	if !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("deploymentName")

	_, found, err := s.getDeploymentRecord(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(deploymentsBucket, deploymentKey(subID, rg, name)); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	// clearOperations recolecta las keys y las borra después de que
	// termina el List (no dentro de su callback): BoltDB no permite abrir
	// una transacción de escritura desde dentro de una de lectura en el
	// mismo goroutine — hacerlo bloquea para siempre.
	if err := s.clearOperations(subID, rg, name); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteAccepted(w, r, s.ops, subID, resourcesProvider, "global", apiVersion)
}
