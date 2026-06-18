package server

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// OperationStatus replica el recurso que ARM expone para hacer polling de
// una operación asíncrona vía la URL devuelta en el header
// Azure-AsyncOperation (status: "InProgress" | "Succeeded" | "Failed").
type OperationStatus struct {
	Status    string    `json:"status"`
	StartTime string    `json:"startTime"`
	EndTime   string    `json:"endTime,omitempty"`
	Error     *APIError `json:"error,omitempty"`
}

// Operations es un registro en memoria de operaciones asíncronas. El
// emulador ejecuta cada mutación de forma síncrona, pero igual expone el
// recurso de operación para que az CLI / Terraform, que hacen polling
// sobre Azure-AsyncOperation o Location, vean Succeeded de inmediato.
type Operations struct {
	mu  sync.Mutex
	seq int64
	ops map[string]*OperationStatus
}

func NewOperations() *Operations {
	return &Operations{ops: make(map[string]*OperationStatus)}
}

// Succeeded registra una nueva operación ya completada y devuelve su ID,
// para usar en las URLs de Azure-AsyncOperation/Location.
func (o *Operations) Succeeded() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	id := fmt.Sprintf("op-%d", o.seq)
	now := time.Now().UTC().Format(time.RFC3339)
	o.ops[id] = &OperationStatus{Status: "Succeeded", StartTime: now, EndTime: now}
	return id
}

// Failed registra una nueva operación fallida y devuelve su ID.
func (o *Operations) Failed(code, message string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	id := fmt.Sprintf("op-%d", o.seq)
	now := time.Now().UTC().Format(time.RFC3339)
	o.ops[id] = &OperationStatus{
		Status: "Failed", StartTime: now, EndTime: now,
		Error: &APIError{Code: code, Message: message},
	}
	return id
}

func (o *Operations) Get(id string) (*OperationStatus, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	op, ok := o.ops[id]
	return op, ok
}

// RegisterOperations monta el endpoint de polling compartido por todos los
// servicios:
//
//	GET /subscriptions/{sub}/providers/{provider}/locations/{location}/operationsStatus/{operationId}
//
// que es el shape real que usa ARM para Azure-AsyncOperation. Se registra
// una sola vez aquí para evitar rutas duplicadas si dos servicios montan
// el mismo path.
func RegisterOperations(mux *http.ServeMux, ops *Operations) {
	mux.HandleFunc("GET /subscriptions/{sub}/providers/{provider}/locations/{location}/operationsStatus/{operationId}",
		func(w http.ResponseWriter, r *http.Request) {
			op, ok := ops.Get(r.PathValue("operationId"))
			if !ok {
				WriteError(w, http.StatusNotFound, "OperationNotFound", "no se encontró la operación solicitada")
				return
			}
			WriteJSON(w, http.StatusOK, op)
		})
}

// AsyncOperationURL construye la URL de polling para una operación,
// siguiendo el shape de Azure-AsyncOperation:
// {scheme}://{host}/subscriptions/{sub}/providers/{provider}/locations/{location}/operationsStatus/{id}?api-version={apiVersion}
func AsyncOperationURL(r *http.Request, sub, provider, location, operationID, apiVersion string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/subscriptions/%s/providers/%s/locations/%s/operationsStatus/%s?api-version=%s",
		scheme, r.Host, sub, provider, location, operationID, apiVersion)
}

// WriteAccepted escribe una respuesta 202 Accepted con los headers
// Azure-AsyncOperation y Location apuntando a la operación dada,
// tal como ARM responde a PUT/DELETE/POST de larga duración.
func WriteAccepted(w http.ResponseWriter, r *http.Request, ops *Operations, sub, provider, location, apiVersion string) {
	id := ops.Succeeded()
	url := AsyncOperationURL(r, sub, provider, location, id, apiVersion)
	w.Header().Set("Azure-AsyncOperation", url)
	w.Header().Set("Location", url)
	w.WriteHeader(http.StatusAccepted)
}
