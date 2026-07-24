package containerinstances

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cesarmarin/azure-emulator/internal/storage"
)

const (
	sub  = "00000000-0000-0000-0000-000000000000"
	rg   = "test-rg"
	api  = "2023-05-01"
	name = "mygroup"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mux := http.NewServeMux()
	New(db).Register(mux)
	return httptest.NewServer(mux)
}

func groupURL(srv *httptest.Server, groupName string) string {
	base := srv.URL + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.ContainerInstance/containerGroups"
	if groupName == "" {
		return base + "?api-version=" + api
	}
	return base + "/" + groupName + "?api-version=" + api
}

func req(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
	} else {
		r, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestContainerGroupCRUD(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	body := `{
		"location": "eastus",
		"properties": {
			"containers": [{
				"name": "mycontainer",
				"properties": {
					"image": "nginx:latest",
					"resources": { "requests": { "cpu": 1.0, "memoryInGB": 1.5 } },
					"ports": [{ "port": 80, "protocol": "TCP" }]
				}
			}],
			"osType": "Linux",
			"restartPolicy": "Always",
			"ipAddress": {
				"type": "Public",
				"ports": [{ "port": 80, "protocol": "TCP" }]
			}
		}
	}`

	// PUT (create) → 201
	resp, err := http.DefaultClient.Do(req(t, "PUT", groupURL(srv, name), body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT want 201 got %d", resp.StatusCode)
	}

	// GET → 200 con IP y FQDN
	resp, err = http.DefaultClient.Do(req(t, "GET", groupURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	var g ContainerGroup
	json.NewDecoder(resp.Body).Decode(&g)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET want 200 got %d", resp.StatusCode)
	}
	if g.Properties.ProvisioningState != "Succeeded" {
		t.Errorf("ProvisioningState = %q", g.Properties.ProvisioningState)
	}
	if len(g.Properties.Containers) != 1 {
		t.Fatalf("want 1 container got %d", len(g.Properties.Containers))
	}
	if g.Properties.Containers[0].Properties.Image != "nginx:latest" {
		t.Errorf("Image = %q", g.Properties.Containers[0].Properties.Image)
	}
	if g.Properties.IPAddress == nil {
		t.Fatal("IPAddress must be set")
	}
	if g.Properties.IPAddress.IP == "" {
		t.Error("IP must not be empty")
	}
	if g.Properties.IPAddress.Fqdn == "" {
		t.Error("Fqdn must not be empty")
	}
	if g.Properties.OsType != "Linux" {
		t.Errorf("OsType = %q", g.Properties.OsType)
	}

	// Determinismo: dos GETs deben devolver la misma IP
	resp2, _ := http.DefaultClient.Do(req(t, "GET", groupURL(srv, name), ""))
	var g2 ContainerGroup
	json.NewDecoder(resp2.Body).Decode(&g2)
	resp2.Body.Close()
	if g.Properties.IPAddress.IP != g2.Properties.IPAddress.IP {
		t.Error("IP must be deterministic")
	}

	// PUT idempotente → 200
	resp, err = http.DefaultClient.Do(req(t, "PUT", groupURL(srv, name), body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT update want 200 got %d", resp.StatusCode)
	}

	// LIST → 1 item
	resp, err = http.DefaultClient.Do(req(t, "GET", groupURL(srv, ""), ""))
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Value []ContainerGroup }
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Value) != 1 {
		t.Fatalf("LIST want 1 got %d", len(list.Value))
	}

	// DELETE → 200
	resp, err = http.DefaultClient.Do(req(t, "DELETE", groupURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE want 200 got %d", resp.StatusCode)
	}

	// GET after delete → 404
	resp, err = http.DefaultClient.Do(req(t, "GET", groupURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete want 404 got %d", resp.StatusCode)
	}

	// DELETE idempotente → 204
	resp, err = http.DefaultClient.Do(req(t, "DELETE", groupURL(srv, name), ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE missing want 204 got %d", resp.StatusCode)
	}
}
