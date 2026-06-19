package graph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestServer() *httptest.Server {
	mux := http.NewServeMux()
	New().Register(mux)
	return httptest.NewServer(mux)
}

type spList struct {
	Value []servicePrincipal `json:"value"`
}

func TestListServicePrincipalsByAppID(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	filter := url.QueryEscape("appId eq 'app-123'")
	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + filter)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 1 || list.Value[0].AppID != "app-123" || list.Value[0].ID == "" {
		t.Fatalf("list: %+v", list.Value)
	}
}

func TestListServicePrincipalsIsDeterministic(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	filter := url.QueryEscape("appId eq 'app-456'")
	getOnce := func() string {
		resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + filter)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		var list spList
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(list.Value) != 1 {
			t.Fatalf("list: %+v", list.Value)
		}
		return list.Value[0].ID
	}

	id1 := getOnce()
	id2 := getOnce()
	if id1 != id2 {
		t.Fatalf("expected stable object id across calls, got %q and %q", id1, id2)
	}
}

func TestListServicePrincipalsWithoutFilterReturnsEmpty(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 0 {
		t.Fatalf("expected empty list, got %+v", list.Value)
	}
}

func TestListServicePrincipalsWithUnparseableFilterReturnsEmpty(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1.0/servicePrincipals?$filter=" + url.QueryEscape("unsupported filter"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var list spList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Value) != 0 {
		t.Fatalf("expected empty list for unparseable filter, got %+v", list.Value)
	}
}
