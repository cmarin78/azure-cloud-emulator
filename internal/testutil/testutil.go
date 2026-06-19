// Package testutil provee helpers compartidos por los tests de cada
// servicio: una base de datos BoltDB descartable por test (aislada, sin
// compartir estado entre tests) y un helper para hacer requests JSON
// contra un httptest.Server igual que lo haría un cliente real
// (az rest/Terraform).
package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

// NewDB abre una base de datos BoltDB nueva dentro de t.TempDir() y
// registra un hook de limpieza que la cierra al terminar el test. Cada
// llamada obtiene su propio archivo, así que los tests pueden correr en
// paralelo sin pisarse el estado.
func NewDB(t *testing.T) *storage.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("testutil.NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// DoJSON hace una petición HTTP contra url, codificando opcionalmente body
// como el payload JSON de la petición (nil para no enviar cuerpo), y
// decodifica la respuesta JSON en out (nil para ignorar el cuerpo).
// Agrega "api-version" a la query string si url no la incluye ya, ya que
// casi todas las rutas ARM de este emulador la exigen.
// Falla el test ante cualquier error de transporte, pero NO ante códigos
// de estado no-2xx: quien llama y le importe el status debe revisarlo.
func DoJSON(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("testutil.DoJSON: marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(data)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("testutil.DoJSON: new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("testutil.DoJSON: %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("testutil.DoJSON: %s %s: decode response: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

// WithAPIVersion agrega "?api-version=2023-01-01" (o "&..." si la URL ya
// tiene query string) — valor arbitrario, el emulador no valida el
// contenido, solo que el parámetro esté presente.
func WithAPIVersion(url string) string {
	sep := "?"
	for i := 0; i < len(url); i++ {
		if url[i] == '?' {
			sep = "&"
			break
		}
	}
	return url + sep + "api-version=2023-01-01"
}
