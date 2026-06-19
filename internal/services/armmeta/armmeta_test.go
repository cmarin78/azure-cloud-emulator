package armmeta

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataEndpointsDocument(t *testing.T) {
	mux := http.NewServeMux()
	New().Register(mux, "http://localhost:10000")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metadata/endpoints")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if doc["name"] != "AzureEmulator" {
		t.Fatalf("expected name field, got %+v", doc)
	}
	if doc["resourceManager"] != "http://localhost:10000/" {
		t.Fatalf("resourceManager: %+v", doc["resourceManager"])
	}
	if doc["portal"] != "http://localhost:10000/" || doc["gallery"] != "http://localhost:10000/" || doc["graph"] != "http://localhost:10000/" {
		t.Fatalf("expected portal/gallery/graph to point back at base, got %+v", doc)
	}

	auth, ok := doc["authentication"].(map[string]any)
	if !ok {
		t.Fatalf("expected authentication object, got %+v", doc["authentication"])
	}
	if auth["loginEndpoint"] != "http://localhost:10000/login/" {
		t.Fatalf("loginEndpoint: %+v", auth["loginEndpoint"])
	}
	if auth["tenant"] != "common" || auth["identityProvider"] != "AAD" {
		t.Fatalf("auth: %+v", auth)
	}

	suffixes, ok := doc["suffixes"].(map[string]any)
	if !ok {
		t.Fatalf("expected suffixes object, got %+v", doc["suffixes"])
	}
	if suffixes["storage"] != "core.windows.net" || suffixes["keyVaultDns"] != "vault.azure.net" {
		t.Fatalf("suffixes: %+v", suffixes)
	}
}
