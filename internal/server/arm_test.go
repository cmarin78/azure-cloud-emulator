package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHealth(mux)

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected ok status, got %q", res["status"])
	}
}

func TestRequireAPIVersion(t *testing.T) {
	t.Run("valid api-version", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?api-version=2021-04-01", nil)
		rec := httptest.NewRecorder()
		ver, ok := RequireAPIVersion(rec, req)
		if !ok || ver != "2021-04-01" {
			t.Fatalf("expected ok=true, ver=2021-04-01, got ok=%v ver=%q", ok, ver)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status code written: %d", rec.Code)
		}
	})

	t.Run("missing api-version", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		ver, ok := RequireAPIVersion(rec, req)
		if ok || ver != "" {
			t.Fatalf("expected ok=false, ver='', got ok=%v ver=%q", ok, ver)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestParseResourceID(t *testing.T) {
	path := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Storage/storageAccounts/acc1"
	id, ok := ParseResourceID(path)
	if !ok {
		t.Fatal("expected ok=true for valid ARM path")
	}
	if id.SubscriptionID != "00000000-0000-0000-0000-000000000000" || id.ResourceGroup != "my-rg" || id.Provider != "Microsoft.Storage" || id.ResourceType != "storageAccounts" || id.ResourceName != "acc1" {
		t.Fatalf("unexpected parsed values: %+v", id)
	}

	// Invalid paths
	_, ok = ParseResourceID("/invalid/path")
	if ok {
		t.Fatal("expected false on invalid ARM path")
	}
}

func TestWriteAccepted(t *testing.T) {
	ops := NewOperations()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "http://localhost:10000/test", nil)

	WriteAccepted(rec, req, ops, "sub-1", "Microsoft.Storage", "eastus", "2023-01-01")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header to be populated")
	}
	if asyncOp := rec.Header().Get("Azure-AsyncOperation"); asyncOp != loc {
		t.Errorf("expected Azure-AsyncOperation header %q to match Location %q", asyncOp, loc)
	}
}

func TestServerMiddleware(t *testing.T) {
	srv := New()
	srv.Mux().HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	srv.Mux().HandleFunc("GET /normal", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"result": "ok"})
	})

	h := srv.Handler()

	// CORS Preflight (OPTIONS)
	reqOpt := httptest.NewRequest("OPTIONS", "/normal", nil)
	recOpt := httptest.NewRecorder()
	h.ServeHTTP(recOpt, reqOpt)
	if recOpt.Code != http.StatusNoContent {
		t.Errorf("OPTIONS expected 204, got %d", recOpt.Code)
	}
	if recOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS origin header *")
	}

	// Normal request
	reqNorm := httptest.NewRequest("GET", "/normal", nil)
	recNorm := httptest.NewRecorder()
	h.ServeHTTP(recNorm, reqNorm)
	if recNorm.Code != http.StatusOK {
		t.Errorf("GET expected 200, got %d", recNorm.Code)
	}

	// Panic recovery
	reqPanic := httptest.NewRequest("GET", "/panic", nil)
	recPanic := httptest.NewRecorder()
	h.ServeHTTP(recPanic, reqPanic)
	if recPanic.Code != http.StatusInternalServerError {
		t.Errorf("panic expected 500, got %d", recPanic.Code)
	}
	var errResp map[string]APIError
	if err := json.Unmarshal(recPanic.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if errResp["error"].Code != "InternalServerError" {
		t.Errorf("expected InternalServerError, got %q", errResp["error"].Code)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusNotFound, "ResourceNotFound", "resource does not exist")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var errResp map[string]APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error JSON: %v", err)
	}
	if errResp["error"].Code != "ResourceNotFound" || errResp["error"].Message != "resource does not exist" {
		t.Fatalf("unexpected APIError: %+v", errResp)
	}
}
