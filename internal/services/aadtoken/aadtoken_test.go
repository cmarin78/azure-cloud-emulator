package aadtoken

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestServer() *httptest.Server {
	mux := http.NewServeMux()
	New().Register(mux)
	return httptest.NewServer(mux)
}

func postForm(t *testing.T, url string, form url.Values) tokenResponse {
	t.Helper()
	resp, err := http.PostForm(url, form)
	if err != nil {
		t.Fatalf("post form: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return tok
}

func decodeJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT with 3 segments, got %d: %q", len(parts), token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload segment: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func TestIssueTokenV2(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	form := url.Values{
		"client_id":     {"client1"},
		"client_secret": {"secret1"},
		"scope":         {"https://management.azure.com/.default"},
		"grant_type":    {"client_credentials"},
	}
	tok := postForm(t, srv.URL+"/login/tenant1/oauth2/v2.0/token", form)
	if tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.AccessToken == "" {
		t.Fatalf("token response: %+v", tok)
	}

	payload := decodeJWTPayload(t, tok.AccessToken)
	if payload["tid"] != "tenant1" || payload["appid"] != "client1" {
		t.Fatalf("payload claims: %+v", payload)
	}
	if payload["aud"] != "https://management.azure.com/.default" {
		t.Fatalf("expected aud from scope, got %+v", payload["aud"])
	}
}

func TestIssueTokenV1UsesResourceParam(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	form := url.Values{
		"client_id":     {"client2"},
		"client_secret": {"secret2"},
		"resource":      {"https://vault.azure.net"},
	}
	tok := postForm(t, srv.URL+"/login/tenant2/oauth2/token", form)
	payload := decodeJWTPayload(t, tok.AccessToken)
	if payload["aud"] != "https://vault.azure.net" {
		t.Fatalf("expected aud from resource, got %+v", payload["aud"])
	}
}

func TestIssueTokenDefaultsAudienceWhenMissing(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	form := url.Values{"client_id": {"client3"}}
	tok := postForm(t, srv.URL+"/login/tenant3/oauth2/v2.0/token", form)
	payload := decodeJWTPayload(t, tok.AccessToken)
	if payload["aud"] != "https://management.core.windows.net/" {
		t.Fatalf("expected default audience, got %+v", payload["aud"])
	}
}

func TestIssueTokenRejectsMalformedBody(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/login/tenant1/oauth2/v2.0/token", "application/x-www-form-urlencoded", strings.NewReader("%zz"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed body, got %d", resp.StatusCode)
	}
}
